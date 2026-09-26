/**
 * Network boundary for the game client.
 *
 * Owns exactly one WebSocket connection per session and is the only place
 * that speaks the protocol. Components never open sockets and PixiJS never
 * sees the transport: both observe the shell store, which the reducer
 * below updates exclusively from server snapshots and events.
 *
 * Rules this module enforces:
 * - one connection, ever (a second call is a no-op, not a second socket)
 * - actions are never retried on their own; a retry would need a new seq
 *   and is the caller's decision after an explicit failure
 * - a rejected action does not change local state (the server decides)
 * - reconnection resyncs from a fresh authoritative snapshot
 */
import {
  PROTOCOL_MAJOR_VERSION,
  type ActionType,
  type GameConfig,
  type GameEvent,
  type GameSnapshot,
} from '@highjack/protocol';

/** Client identity sent in the protocol handshake. */
const CLIENT_ID = 'game/0.2.0';

export type ConnectionStatus =
  'idle' | 'connecting' | 'handshaking' | 'connected' | 'reconnecting' | 'disconnected' | 'error';

export interface NetHandlers {
  onStatus(status: ConnectionStatus, detail?: string): void;
  onWelcome(session: string): void;
  onSnapshot(snapshot: GameSnapshot, config: GameConfig, nextSeq: number, resync: boolean): void;
  onEvents(events: readonly { tick: number; event: GameEvent }[]): void;
  /** Called with the authoritative state the action produced (on success). */
  onActionResult(
    seq: number,
    ok: boolean,
    code?: string,
    message?: string,
    snapshot?: GameSnapshot,
  ): void;
}

interface Pending {
  resolve: (ok: boolean) => void;
}

export class GameConnection {
  private socket: WebSocket | null = null;
  private seq = 0;
  private nextSeq = 1;
  private closing = false;
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly pending = new Map<number, Pending>();

  constructor(
    private readonly url: string,
    private readonly matchId: string,
    private readonly token: string,
    private readonly handlers: NetHandlers,
  ) {}

  connect(): void {
    // Guard on the raw numeric readyState values so the single-connection
    // invariant never depends on the WebSocket constructor exposing its
    // constants on a substituted implementation.
    const state = this.socket?.readyState;
    if (state === 0 /* CONNECTING */ || state === 1 /* OPEN */) {
      return; // exactly one connection, ever
    }
    this.closing = false;
    this.handlers.onStatus(this.reconnectAttempt > 0 ? 'reconnecting' : 'connecting');
    const socket = new WebSocket(this.url);
    this.socket = socket;

    socket.addEventListener('open', () => {
      this.handlers.onStatus('handshaking');
      // The protocol requires hello-first: the server binds this
      // connection to a match and answers with welcome + snapshot.
      socket.send(
        JSON.stringify({
          v: PROTOCOL_MAJOR_VERSION,
          type: 'hello',
          client: CLIENT_ID,
          matchId: this.matchId,
          token: this.token,
        }),
      );
    });
    socket.addEventListener('message', (ev) => this.onMessage(String(ev.data)));
    socket.addEventListener('close', () => this.onClose());
    socket.addEventListener('error', () => this.handlers.onStatus('error', 'socket error'));
  }

  private onMessage(raw: string): void {
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(raw) as Record<string, unknown>;
    } catch {
      this.handlers.onStatus('error', 'malformed server frame');
      return;
    }
    switch (msg['type']) {
      case 'welcome':
        this.reconnectAttempt = 0;
        this.handlers.onWelcome(String(msg['session'] ?? ''));
        this.handlers.onStatus('connected');
        break;
      case 'snapshot':
        this.nextSeq = Number(msg['nextSeq'] ?? this.nextSeq) || this.nextSeq;
        this.handlers.onSnapshot(
          msg['snapshot'] as GameSnapshot,
          msg['config'] as GameConfig,
          this.nextSeq,
          msg['resync'] === true,
        );
        break;
      case 'catchup': {
        const events = (msg['events'] as { tick: number; event: GameEvent }[] | undefined) ?? [];
        if (events.length > 0) this.handlers.onEvents(events);
        break;
      }
      case 'transition': {
        // One frame per authoritative transition. Applied as a batch so a
        // transition cannot be interleaved with another, which is what the
        // server now guarantees by construction.
        const events = (msg['events'] as { tick: number; event: GameEvent }[] | undefined) ?? [];
        if (events.length > 0) this.handlers.onEvents(events);
        break;
      }
      case 'event': {
        // Retained for compatibility with a server that still sends one event per
        // frame. The batched form above is what v0.2.2 emits.
        const tick = Number(msg['tick'] ?? 0);
        const event = msg['event'] as GameEvent;
        if (event) this.handlers.onEvents([{ tick, event }]);
        break;
      }
      case 'ack': {
        const ackSeq = Number(msg['ackSeq'] ?? 0);
        const next = Number(msg['nextSeq'] ?? ackSeq + 1);
        this.nextSeq = Math.max(this.nextSeq, next);
        this.pending.get(ackSeq)?.resolve(true);
        this.pending.delete(ackSeq);
        // The ack carries the authoritative post-action state.
        this.handlers.onActionResult(
          ackSeq,
          true,
          undefined,
          undefined,
          msg['snapshot'] as GameSnapshot,
        );
        break;
      }
      case 'error': {
        const err = msg['error'] as { code?: string; message?: string } | undefined;
        const ackSeq = typeof msg['ackSeq'] === 'number' ? msg['ackSeq'] : undefined;
        if (ackSeq !== undefined) {
          this.pending.get(ackSeq)?.resolve(false);
          this.pending.delete(ackSeq);
        }
        this.handlers.onActionResult(ackSeq ?? -1, false, err?.code, err?.message);
        break;
      }
      case 'pong':
        break;
      default:
        break;
    }
  }

  private onClose(): void {
    this.socket = null;
    for (const [, p] of this.pending) p.resolve(false);
    this.pending.clear();
    if (this.closing) {
      this.handlers.onStatus('disconnected');
      return;
    }
    this.handlers.onStatus('reconnecting');
    this.reconnectAttempt += 1;
    const delay = Math.min(500 * 2 ** (this.reconnectAttempt - 1), 8000);
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  /** Sends an action and resolves true only when the server acks it. */
  send(type: ActionType, extra: Record<string, unknown> = {}): Promise<boolean> {
    if (!this.socket || this.socket.readyState !== 1 /* OPEN */) return Promise.resolve(false);
    const seq = this.seq + 1;
    this.seq = seq;
    this.nextSeq = Math.max(this.nextSeq, seq);
    const frame = JSON.stringify({
      v: PROTOCOL_MAJOR_VERSION,
      type: 'action',
      seq,
      action: { type, ...extra },
    });
    return new Promise<boolean>((resolve) => {
      this.pending.set(seq, { resolve });
      this.socket?.send(frame);
    });
  }

  /** Next sequence number the client will use (diagnostics/tests). */
  peekNextSeq(): number {
    return this.seq + 1;
  }

  close(): void {
    this.closing = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.socket?.close(1000, 'client leaving');
    this.socket = null;
    this.handlers.onStatus('idle');
  }
}

/** The public lobby API the client uses to enter a match. */
export interface CreatedMatch {
  matchId: string;
  configHash: string;
}

export interface JoinedSeat {
  playerId: string;
  token: string;
  matchId: string;
  seat: number;
}

export async function createMatch(serverUrl: string): Promise<CreatedMatch> {
  const res = await fetch(`${serverUrl}/matches`, { method: 'POST' });
  if (!res.ok) throw new Error(`match creation failed (${res.status})`);
  return (await res.json()) as CreatedMatch;
}

export async function joinMatch(
  serverUrl: string,
  matchId: string,
  displayName: string,
): Promise<JoinedSeat> {
  const res = await fetch(`${serverUrl}/matches/${encodeURIComponent(matchId)}/players`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ displayName }),
  });
  if (!res.ok) throw new Error(`join failed (${res.status})`);
  return (await res.json()) as JoinedSeat;
}
