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
  /**
   * The engine's current legal actions for this connection.
   *
   * Delivered on every transition, not only on a snapshot, because the legality
   * of *this* player can change when *someone else* acts — a second player
   * readying is what unblocks the host's Start.
   */
  onLegalActions(actions: readonly ActionType[]): void;
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
        // One frame per authoritative transition, per recipient. Applied as a batch
        // so a transition cannot be interleaved with another, which is what the
        // server guarantees by construction.
        const events = (msg['events'] as { tick: number; event: GameEvent }[] | undefined) ?? [];
        if (events.length > 0) this.handlers.onEvents(events);
        // The frame also carries *this* recipient's legal actions. Without it the
        // dock only refreshed on a snapshot, so it went stale the moment another
        // player acted — a host whose second player had just readied still saw a
        // disabled Start, with nothing to indicate why.
        const you = msg['you'] as { legalActions?: ActionType[] } | undefined;
        if (you && Array.isArray(you.legalActions)) {
          this.handlers.onLegalActions(you.legalActions);
        }
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

/**
 * A seat this browser holds at a private table.
 *
 * The token is a bearer credential, so it is stored under its own key and scoped
 * to the match id rather than bundled into general client state. It is never the
 * match's root seed, which stays server-side.
 */
export interface StoredSeat {
  readonly matchId: string;
  readonly token: string;
  readonly playerId: string;
}

const SEAT_KEY = 'highjack.seat.v1';

/**
 * How many seats are kept.
 *
 * A seat is a bearer credential that never expires and is never revoked, so
 * keeping every one a browser ever took is a liability rather than a feature.
 * The cap keeps the useful case — the table you are at, and the one you just
 * left — without accumulating a pile of tokens.
 */
const MAX_STORED_SEATS = 8;

type Storage = Pick<globalThis.Storage, 'getItem' | 'setItem' | 'removeItem'>;

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    // Blocked by browser settings. Not fatal: the seat simply will not survive a
    // reload, and the player can rejoin by match id.
    return null;
  }
}

/** The seats this browser holds, keyed by match id. */
interface StoredSeats {
  [matchId: string]: { token: string; playerId: string };
}

/**
 * Read every stored seat.
 *
 * The seats are keyed by match id rather than held as one value, so joining a
 * second table no longer discards the first. A single global key meant that
 * joining a new match overwrote the old seat, and a reload always returned to
 * whichever table was joined last — including one that had already ended.
 */
function readSeats(): StoredSeats {
  const raw = storage()?.getItem(SEAT_KEY);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (typeof parsed !== 'object' || parsed === null) return {};
    const out: StoredSeats = {};
    for (const [matchId, seat] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof seat !== 'object' || seat === null) continue;
      const { token, playerId } = seat as { token?: unknown; playerId?: unknown };
      if (
        typeof token !== 'string' ||
        typeof playerId !== 'string' ||
        token === '' ||
        playerId === ''
      ) {
        continue;
      }
      out[matchId] = { token, playerId };
    }
    return out;
  } catch {
    return {};
  }
}

function writeSeats(seats: StoredSeats): void {
  const store = storage();
  if (!store) return;
  const ids = Object.keys(seats);
  if (ids.length === 0) {
    store.removeItem(SEAT_KEY);
    return;
  }
  store.setItem(SEAT_KEY, JSON.stringify(seats));
}

/** Persist a seat so a reload rejoins the same table. */
export function rememberSeat(matchId: string, token: string, playerId: string): void {
  if (matchId === '' || token === '' || playerId === '') return;
  const seats = readSeats();
  seats[matchId] = { token, playerId };
  // Keep the most recently stored seats. Object key order is insertion order
  // for string keys, so the first entries are the oldest.
  const ids = Object.keys(seats);
  if (ids.length > MAX_STORED_SEATS) {
    for (const id of ids.slice(0, ids.length - MAX_STORED_SEATS)) delete seats[id];
  }
  writeSeats(seats);
}

/**
 * Read a stored seat.
 *
 * With a match id, the seat for that table. Without one, the most recently
 * stored seat — which is the one a reload should return to.
 */
export function readStoredSeat(matchId?: string): StoredSeat | null {
  const seats = readSeats();
  const ids = Object.keys(seats);
  if (ids.length === 0) return null;
  const id = matchId ?? ids[ids.length - 1] ?? '';
  const seat = seats[id];
  if (!seat) return null;
  return { matchId: id, token: seat.token, playerId: seat.playerId };
}

/**
 * Forget a stored seat, once a match has ended or the player has left.
 *
 * This used to exist and be called from nowhere, so a seat — and the bearer
 * token in it — stayed in local storage for the life of the browser, and a
 * reload always returned the player to a table they had already left.
 */
export function forgetSeat(matchId?: string): void {
  const store = storage();
  if (!store) return;
  if (matchId === undefined) {
    store.removeItem(SEAT_KEY);
    return;
  }
  const seats = readSeats();
  delete seats[matchId];
  writeSeats(seats);
}
