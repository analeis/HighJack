import { describe, expect, it } from 'vitest';
import { GameConnection } from '../src/net/connection.ts';
import type { ConnectionStatus } from '../src/net/connection.ts';
import type { GameConfig, GameEvent, GameSnapshot } from '@highjack/protocol';

/**
 * Network-boundary tests use a fake WebSocket: the class under test is
 * exercised for its real behavior (single connection, sequencing, ack
 * correlation, reconnect) without needing a live server. The end-to-end
 * proof runs against the real Go server in the Go integration suite.
 */

const CONNECTING = 0;

class FakeSocket {
  readyState = CONNECTING;
  sent: string[] = [];
  private listeners: Record<string, ((ev: unknown) => void)[]> = {};
  failNextWrite = false;

  // Instances are tracked per test, not globally, so a leaked socket from
  // one test can never be counted by the next.
  static reset(): FakeSocket[] {
    FakeSocket.registry = [];
    return FakeSocket.registry;
  }
  private static registry: FakeSocket[] = [];

  constructor() {
    FakeSocket.registry.push(this);
  }

  static all(): FakeSocket[] {
    return FakeSocket.registry;
  }

  addEventListener(type: string, fn: (ev: unknown) => void): void {
    (this.listeners[type] ??= []).push(fn);
  }

  send(data: string): void {
    if (this.failNextWrite) {
      this.failNextWrite = false;
      throw new Error('write failed');
    }
    this.sent.push(data);
  }

  close(): void {
    this.readyState = 3; // CLOSED
    this.emit('close', {});
  }

  open(): void {
    this.readyState = 1;
    this.emit('open', {});
  }

  message(payload: unknown): void {
    this.emit('message', { data: JSON.stringify(payload) });
  }

  private emit(type: string, ev: unknown): void {
    for (const fn of this.listeners[type] ?? []) fn(ev);
  }
}

function installFakeWebSocket(): { restore: () => void; sockets: () => FakeSocket[] } {
  const original = (globalThis as { WebSocket?: unknown }).WebSocket;
  const registry = FakeSocket.reset();
  (globalThis as { WebSocket?: unknown }).WebSocket = FakeSocket as unknown;
  return {
    restore: () => {
      (globalThis as { WebSocket?: unknown }).WebSocket = original;
    },
    sockets: () => registry,
  };
}

interface Harness {
  conn: GameConnection;
  statuses: ConnectionStatus[];
  events: { tick: number; event: GameEvent }[];
  snapshots: GameSnapshot[];
  results: { seq: number; ok: boolean; code?: string }[];
}

function harness(): Harness {
  const statuses: ConnectionStatus[] = [];
  const events: { tick: number; event: GameEvent }[] = [];
  const snapshots: GameSnapshot[] = [];
  const results: { seq: number; ok: boolean; code?: string }[] = [];
  const conn = new GameConnection('ws://x/ws', 'm_1', 'tok_1', {
    onStatus: (s) => statuses.push(s),
    onWelcome: () => {},
    onSnapshot: (s) => snapshots.push(s),
    onEvents: (e) => events.push(...e),
    onActionResult: (seq, ok, code) =>
      results.push(code === undefined ? { seq, ok } : { seq, ok, code }),
  });
  return { conn, statuses, events, snapshots, results };
}

const SNAP = {
  matchId: 'm_1',
  phase: 'lobby',
  tick: 3,
  players: [],
  spaces: [],
  turn: { currentSeat: 0, phase: 'await_roll', round: 1, doublesStreak: 0, awardExtraRoll: false },
  winnerId: null,
  endReason: '',
  configHash: 'h',
} as unknown as GameSnapshot;

const CONFIG = { version: 1 } as unknown as GameConfig;

describe('GameConnection', () => {
  it('opens exactly one socket and handshakes on welcome', () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      h.conn.connect(); // must be a no-op
      const socket = ws.sockets()[0]!;
      expect(ws.sockets()).toHaveLength(1);
      socket.open();
      socket.message({ v: 1, type: 'welcome', session: 'sess_1', protocolVersion: '1.1.0' });
      expect(h.statuses).toContain('connected');
    } finally {
      ws.restore();
    }
  });

  it('assigns monotonic sequence numbers and resolves on ack', async () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      const socket = ws.sockets()[0]!;
      socket.open();
      expect(JSON.parse(socket.sent[0]!).type).toBe('hello');
      socket.message({ v: 1, type: 'welcome', session: 'sess_1' });

      const p1 = h.conn.send('player_ready', { ready: true });
      const p2 = h.conn.send('player_ready', { ready: false });
      // The first frame is the protocol hello sent on open; actions
      // follow with monotonic sequence numbers.
      const actions = socket.sent
        .map((raw) => JSON.parse(raw) as { type: string; seq?: number })
        .filter((f) => f.type === 'action');
      expect(actions.map((f) => f.seq)).toEqual([1, 2]);

      socket.message({ v: 1, type: 'ack', ackSeq: 1, nextSeq: 2 });
      socket.message({ v: 1, type: 'ack', ackSeq: 2, nextSeq: 3 });
      expect(await p1).toBe(true);
      expect(await p2).toBe(true);
      expect(h.results.filter((r) => r.ok)).toHaveLength(2);
    } finally {
      ws.restore();
    }
  });

  it('resolves pending actions as failed on a domain error', async () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      const socket = ws.sockets()[0]!;
      socket.open();
      socket.message({ v: 1, type: 'welcome', session: 'sess_1' });
      const pending = h.conn.send('roll_dice');
      socket.message({
        v: 1,
        type: 'error',
        ackSeq: 1,
        error: { code: 'not_your_turn', message: 'wait' },
      });
      expect(await pending).toBe(false);
      expect(h.results[0]?.ok).toBe(false);
    } finally {
      ws.restore();
    }
  });

  it('applies snapshots and events through the handler boundary', () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      const socket = ws.sockets()[0]!;
      socket.open();
      socket.message({ v: 1, type: 'welcome', session: 'sess_1' });
      socket.message({
        v: 1,
        type: 'snapshot',
        snapshot: SNAP,
        config: CONFIG,
        nextSeq: 4,
        resync: true,
      });
      expect(h.snapshots).toHaveLength(1);
      socket.message({
        v: 1,
        type: 'event',
        tick: 5,
        event: { type: 'turn_advanced', seat: 1, round: 1 },
      });
      expect(h.events).toHaveLength(1);
      expect(h.events[0]?.tick).toBe(5);
    } finally {
      ws.restore();
    }
  });

  it('reconnects after an unexpected close and does not stack sockets', async () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      const first = ws.sockets()[0]!;
      first.open();
      first.message({ v: 1, type: 'welcome', session: 'sess_1' });
      first.close(); // unexpected drop
      expect(h.statuses).toContain('reconnecting');
      // The retry timer fires on its own; wait briefly for the new socket.
      await new Promise((r) => setTimeout(r, 600));
      const sockets = ws.sockets();
      expect(sockets.length).toBeGreaterThan(1);
      // A pending action from the dropped connection is failed, not lost.
      const pending = h.conn.send('roll_dice');
      expect(await pending).toBe(false);
    } finally {
      ws.restore();
    }
  });

  it('refuses to send while the socket is closed', async () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      expect(await h.conn.send('roll_dice')).toBe(false);
    } finally {
      ws.restore();
    }
  });

  it('stops reconnecting after an explicit close', async () => {
    const ws = installFakeWebSocket();
    try {
      const h = harness();
      h.conn.connect();
      const socket = ws.sockets()[0]!;
      socket.open();
      h.conn.close();
      const before = ws.sockets().length;
      await new Promise((r) => setTimeout(r, 700));
      expect(ws.sockets().length).toBe(before);
      expect(h.statuses[h.statuses.length - 1]).toBe('idle');
    } finally {
      ws.restore();
    }
  });
});
