/**
 * Authoritative client state.
 *
 * One rule: local state changes only through `applySnapshot` and
 * `applyEvents`. UI clicks call the network layer and wait for the server;
 * they never mutate balances, ownership, positions, or turn. Snapshots are
 * authoritative and replace local state wholesale; events are applied
 * through the same pure reducer so a reconnect converges to server truth
 * rather than drifting.
 */
import { createSignal } from 'solid-js';
import type {
  ActionType,
  MatchPhase,
  GameConfig,
  GameEvent,
  GameSnapshot,
} from '@highjack/protocol';
import type { ConnectionStatus } from '../net/connection.ts';

export interface UiPlayer {
  id: string;
  name: string;
  seat: number;
  money: number;
  status: 'active' | 'eliminated' | 'disconnected';
  ready: boolean;
  isHost: boolean;
  position: number;
}

export interface UiSpace {
  id: string;
  kind: 'go' | 'property' | 'tax' | 'neutral';
  name: string;
  group: string;
  price: number;
  rent: number;
  amount: number;
  owned: boolean;
  owner: string;
  level: number;
}

export interface UiTurn {
  currentSeat: number;
  phase: 'await_roll' | 'await_buy_decision' | 'turn_over';
  round: number;
  doublesStreak: number;
  awardExtraRoll: boolean;
}

export interface GameView {
  matchId: string;
  /**
   * Taken from the shared protocol rather than re-declared. A local copy of this
   * union is a third place to update whenever a phase is added, and it silently
   * went stale: the engine could report an interrupted match while this type said
   * it was impossible.
   */
  phase: MatchPhase;
  tick: number;
  /**
   * The action types the engine would accept from this player right now, as
   * reported by the server. The dock renders these; it does not re-derive them.
   */
  legalActions: readonly ActionType[];
  players: UiPlayer[];
  spaces: UiSpace[];
  turn: UiTurn;
  winnerId: string | null;
  endReason: string;
  playerId: string;
  lastRoll: { die1: number; die2: number; byId: string } | null;
  config: GameConfig | null;
}

// Local identity is stored independently of the view: a snapshot can
// arrive before or after the join resolves, and it must never be lost.
const [localPlayerId, setLocalPlayerId] = createSignal('');
const [view, setView] = createSignal<GameView | null>(null);
const [status, setStatus] = createSignal<ConnectionStatus>('idle');
const [statusDetail, setStatusDetail] = createSignal<string>('');
const [lastError, setLastError] = createSignal<string>('');
const [pendingAction, setPendingAction] = createSignal(false);
const [cursor, setCursor] = createSignal(0);
const [resynced, setResynced] = createSignal(false);
const [fps, setFps] = createSignal(0);
const [stageReady, setStageReady] = createSignal(false);
const [reducedMotion, setReducedMotion] = createSignal(
  typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches,
);

function toView(
  snapshot: GameSnapshot,
  config: GameConfig | null,
  prev: GameView | null = null,
): GameView {
  return {
    matchId: snapshot.matchId,
    phase: snapshot.phase,
    tick: snapshot.tick,
    players: snapshot.players.map((p) => ({
      id: p.playerId,
      name: p.name,
      seat: p.seat,
      money: p.money,
      status: p.status,
      ready: p.ready,
      isHost: p.isHost,
      position: p.position,
    })),
    spaces: snapshot.spaces.map((s) => ({
      id: s.id,
      kind: s.kind,
      name: s.name,
      group: s.group,
      price: s.price,
      rent: s.rent,
      amount: s.amount,
      owned: s.owned,
      owner: s.owner,
      level: s.level,
    })),
    turn: {
      currentSeat: snapshot.turn.currentSeat,
      phase: snapshot.turn.phase,
      round: snapshot.turn.round,
      doublesStreak: snapshot.turn.doublesStreak,
      awardExtraRoll: snapshot.turn.awardExtraRoll,
    },
    winnerId: snapshot.winnerId,
    endReason: snapshot.endReason,
    // Always the current local identity, never a stale copy from a
    // previous snapshot.
    playerId: localPlayerId(),
    lastRoll: prev?.lastRoll ?? null,
    // Absent `you` means an older server: fall back to deriving, which is what
    // this client had to do before, rather than showing a dead dock.
    legalActions: snapshot.you?.legalActions ?? derivedLegalActions(snapshot, localPlayerId()),
    config,
  };
}

/**
 * Fallback legality for a server that does not send `you`.
 *
 * This duplicates the engine's rules, and it is only a compatibility path for a
 * protocol 1.1 server. It exists so a newer client against an older server still
 * has a working dock; for a current server it is never called, so it is not a
 * second source of truth.
 */
function derivedLegalActions(snapshot: GameSnapshot, playerId: string): readonly ActionType[] {
  if (!playerId) return [];
  const me = snapshot.players.find((p) => p.playerId === playerId);
  if (!me) return [];
  if (snapshot.phase === 'lobby') {
    return me.isHost
      ? ['player_ready', 'game_start', 'player_leave']
      : ['player_ready', 'player_leave'];
  }
  if (snapshot.phase !== 'playing' || me.status !== 'active') return ['player_leave'];
  if (me.seat !== snapshot.turn.currentSeat) return ['player_leave'];
  switch (snapshot.turn.phase) {
    case 'await_roll':
      return ['roll_dice', 'player_leave'];
    case 'turn_over':
      return ['end_turn', 'player_leave'];
    case 'await_buy_decision': {
      const space = snapshot.spaces[me.position];
      if (!space || space.kind !== 'property' || space.owned) return ['player_leave'];
      return me.money >= space.price
        ? ['buy_property', 'decline_buy', 'player_leave']
        : ['decline_buy', 'player_leave'];
    }
    default:
      return ['player_leave'];
  }
}

/** Installs an authoritative snapshot. The only wholesale state setter. */
export function applySnapshot(
  snapshot: GameSnapshot,
  config: GameConfig | null,
  nextSeq: number,
  resync: boolean,
): void {
  setView((prev) => {
    // A snapshot older than what we hold must never overwrite it. The server
    // publishes a transition's events and then acks the action, and a peer's
    // transition can land between those two writes, so the ack's snapshot can
    // genuinely predate state already on screen. Accepting it rewound money,
    // ownership and the turn pointer with nothing to indicate it had happened.
    if (prev && snapshot.tick < prev.tick) return prev;
    return toView(snapshot, config, prev);
  });
  setCursor((c) => Math.max(c, snapshot.tick));
  setResynced(resync);
  void nextSeq;
}

/**
 * Applied ticks.
 *
 * A transition arrives as one batch whose events all share a tick, so the tick is
 * the transition's identity on the wire and the sound dedup key.
 */
let appliedTicks = new Set<number>();

/**
 * Forget which ticks have been applied.
 *
 * Test-only, so each case starts from a known dedup state; a leaked set would make
 * one test's transition look already-applied in the next.
 */
export function resetAppliedTicks(): void {
  appliedTicks = new Set<number>();
}

function markApplied(tick: number): void {
  appliedTicks.add(tick);
  // Bounded by size only. Pruning by "the view has already reached this tick"
  // looks like an optimisation and is exactly wrong: the tick a view is *at* is
  // the one most likely to be re-delivered, because the acting client receives
  // its own transition's events before the acknowledgement's snapshot arrives.
  // Dropping that entry is the same as having no dedup at all.
  if (appliedTicks.size > 4096) {
    appliedTicks = new Set([...appliedTicks].slice(-2048));
  }
}

/** Applies authoritative events through the pure reducer. */
/**
 * Applies authoritative events.
 *
 * A tick is applied at most once. The server documents at-least-once delivery and
 * names the tick as the dedup key, but the client implemented no dedup at all, so
 * the first re-delivery charged a rent twice or marked a property bought twice.
 */
/**
 * Replace the legal actions the engine reported for this connection.
 *
 * Separate from `applySnapshot` because legality changes when *anyone* acts, not
 * only when this player does: a second player readying is what enables the host's
 * Start, and no snapshot is sent for another player's action.
 */
export function applyLegalActions(actions: readonly ActionType[]): void {
  setView((prev) => (prev ? { ...prev, legalActions: actions } : prev));
}

export function applyEvents(events: readonly { tick: number; event: GameEvent }[]): void {
  if (events.length === 0) return;
  // A transition arrives as one batch whose events all share a tick, so the first
  // entry's tick is the batch's identity.
  const batchTick = events[0]!.tick;

  if (!appliedTicks.has(batchTick)) {
    setView((prev) => {
      if (!prev) return prev;
      let next: GameView = {
        ...prev,
        players: prev.players.map((p) => ({ ...p })),
        spaces: prev.spaces.map((s) => ({ ...s })),
        turn: { ...prev.turn },
      };
      let at = prev.tick;
      for (const { tick: t, event } of events) {
        at = Math.max(at, t);
        next = reduceEvent(next, event);
      }
      next.tick = at;
      return next;
    });
  }
  markApplied(batchTick);
  setCursor((c) => Math.max(c, batchTick));
}

function reduceEvent(state: GameView, event: GameEvent): GameView {
  const move = (id: string, pos: number): void => {
    state.players = state.players.map((p) => (p.id === id ? { ...p, position: pos } : p));
  };
  const adjust = (id: string, delta: number): void => {
    state.players = state.players.map((p) =>
      p.id === id ? { ...p, money: Math.max(0, p.money + delta) } : p,
    );
  };
  const indexOfSpace = (id: string): number => state.spaces.findIndex((s) => s.id === id);

  switch (event.type) {
    case 'player_joined':
      if (!state.players.some((p) => p.id === event.playerId)) {
        state.players = [
          ...state.players,
          {
            id: event.playerId,
            name: event.name,
            seat: event.seat,
            money: event.startingMoney,
            status: 'active',
            ready: false,
            isHost: false,
            position: 0,
          },
        ];
      }
      break;
    case 'player_ready_changed':
      state.players = state.players.map((p) =>
        p.id === event.playerId ? { ...p, ready: event.ready } : p,
      );
      break;
    case 'player_left':
    case 'player_eliminated':
    case 'player_bankrupt':
      state.players = state.players.map((p) =>
        p.id === event.playerId ? { ...p, status: 'eliminated' } : p,
      );
      if (event.type === 'player_bankrupt') {
        // Holdings revert to the bank on bankruptcy.
        state.spaces = state.spaces.map((s) =>
          s.owner === event.playerId ? { ...s, owned: false, owner: '' } : s,
        );
      }
      break;
    case 'game_started':
      state.phase = 'playing';
      break;
    case 'dice_rolled': {
      const idx = indexOfSpace(event.toSpace);
      move(
        event.playerId,
        idx >= 0 ? idx : (state.players.find((p) => p.id === event.playerId)?.position ?? 0),
      );
      state.lastRoll = { die1: event.die1, die2: event.die2, byId: event.playerId };
      break;
    }
    case 'property_bought': {
      const idx = indexOfSpace(event.spaceId);
      if (idx >= 0) {
        state.spaces = state.spaces.map((s, i) =>
          i === idx ? { ...s, owned: true, owner: event.playerId } : s,
        );
      }
      adjust(event.playerId, -event.price);
      break;
    }
    case 'buy_declined':
      break;
    case 'rent_paid':
      adjust(event.fromPlayerId, -event.amount);
      adjust(event.toPlayerId, event.amount);
      break;
    case 'bank_transfer':
      adjust(event.playerId, event.direction === 'to_player' ? event.amount : -event.amount);
      break;
    case 'turn_advanced':
      state.turn = {
        currentSeat: event.seat,
        phase: 'await_roll',
        round: event.round,
        doublesStreak: 0,
        awardExtraRoll: false,
      };
      break;
    case 'game_ended':
      state.phase = 'ended';
      state.winnerId = event.winnerId;
      state.endReason = event.reason;
      break;
    default:
      break;
  }
  return state;
}

/** Local identity (session binding), set once at join. */
export function setLocalPlayer(playerId: string): void {
  setLocalPlayerId(playerId);
  // Re-project immediately so an already-rendered view picks the identity
  // up without waiting for the next server event.
  setView((prev) => (prev ? { ...prev, playerId } : prev));
}

/**
 * Presentation-only helpers. These describe *what to display*, never what is
 * permitted: legality comes from the snapshot's `you.legalActions`.
 *
 * The `canBuy` / `canRoll` / `isMyTurn` family that used to live here was deleted
 * deliberately. Each was a second copy of an engine predicate, and the copies had
 * already drifted: `canBuy` gated Decline, which the engine does not gate, so a
 * player who could not afford a property had no legal move at all while the server
 * was waiting for one. See `you.legalActions` in GameView.
 */
export function currentPlayer(state: GameView | null): UiPlayer | null {
  if (!state) return null;
  return state.players.find((p) => p.seat === state.turn.currentSeat) ?? null;
}

export function myHoldings(state: GameView | null): UiSpace[] {
  if (!state) return [];
  return state.spaces.filter((s) => s.owned && s.owner === state.playerId);
}

export const gameStore = {
  view,
  status,
  statusDetail,
  lastError,
  pendingAction,
  cursor,
  resynced,
  fps,
  stageReady,
  reducedMotion,
  setStatus: (s: ConnectionStatus, detail = '') => {
    setStatus(s);
    setStatusDetail(detail);
  },
  setLastError,
  setPendingAction,
  setFps,
  setStageReady,
  setReducedMotion,
  /**
   * Return the store to its initial state.
   *
   * Tests need this because the store is a module-level singleton, and a leaked
   * view or dedup set between cases would let one test's state satisfy another's
   * assertions. Production never calls it.
   */
  reset: () => {
    setView(null);
    setCursor(0);
    setResynced(false);
    setLastError('');
    setPendingAction(false);
    setStatus('idle');
    setStatusDetail('');
  },
};
