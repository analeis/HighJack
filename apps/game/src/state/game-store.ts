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
import type { MatchPhase, GameConfig, GameEvent, GameSnapshot } from '@highjack/protocol';
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

function toView(snapshot: GameSnapshot, config: GameConfig | null): GameView {
  const prev = view();
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
    config,
  };
}

/** Installs an authoritative snapshot. The only wholesale state setter. */
export function applySnapshot(
  snapshot: GameSnapshot,
  config: GameConfig | null,
  nextSeq: number,
  resync: boolean,
): void {
  setView(toView(snapshot, config));
  setCursor(snapshot.tick);
  setResynced(resync);
  void nextSeq;
}

/** Applies authoritative events through the pure reducer. */
export function applyEvents(events: readonly { tick: number; event: GameEvent }[]): void {
  setView((prev) => {
    if (!prev) return prev;
    let next: GameView = {
      ...prev,
      players: prev.players.map((p) => ({ ...p })),
      spaces: prev.spaces.map((s) => ({ ...s })),
      turn: { ...prev.turn },
    };
    let tick = prev.tick;
    for (const { tick: t, event } of events) {
      tick = Math.max(tick, t);
      next = reduceEvent(next, event);
    }
    next.tick = tick;
    return next;
  });
  const last = events[events.length - 1];
  if (last) setCursor(last.tick);
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

/** Presentation-only helpers (never authoritative). */
export function isMyTurn(state: GameView | null): boolean {
  if (!state || state.phase !== 'playing') return false;
  const me = state.players.find((p) => p.id === state.playerId);
  return me !== undefined && me.status === 'active' && me.seat === state.turn.currentSeat;
}

export function canRoll(state: GameView | null): boolean {
  return isMyTurn(state) && state?.turn.phase === 'await_roll';
}

export function canBuy(state: GameView | null): boolean {
  if (!isMyTurn(state) || state?.turn.phase !== 'await_buy_decision') return false;
  const me = state.players.find((p) => p.id === state.playerId);
  if (!me) return false;
  const space = state.spaces[me.position];
  return (
    space !== undefined && space.kind === 'property' && !space.owned && me.money >= space.price
  );
}

export function canEndTurn(state: GameView | null): boolean {
  return isMyTurn(state) && state?.turn.phase === 'turn_over';
}

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
};
