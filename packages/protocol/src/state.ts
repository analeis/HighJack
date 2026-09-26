/**
 * Authoritative match state snapshots.
 *
 * Snapshots mirror `GameState` in server/internal/game. They are produced
 * exclusively by the server: clients render from them and never construct
 * them. Runtime guards validate the shapes a client receives; the engine
 * remains the sole authority over the values.
 */
import { ACTION_TYPES, type ActionType } from './actions.ts';
import type { BoardSpace, SpaceKind } from './config.ts';
import type { Money, PlayerId, Seat } from './ids.ts';

export type PlayerStatus = 'active' | 'eliminated' | 'disconnected';

export type TurnPhase = 'await_roll' | 'await_buy_decision' | 'turn_over';

export interface SnapshotPlayer {
  readonly playerId: PlayerId;
  readonly name: string;
  readonly seat: Seat;
  readonly money: Money;
  readonly status: PlayerStatus;
  readonly ready: boolean;
  readonly isHost: boolean;
  readonly position: number;
}

export interface SnapshotSpace extends BoardSpace {
  readonly owned: boolean;
  /** Meaningful only when owned is true. */
  readonly owner: PlayerId;
  readonly level: number;
}

export interface SnapshotTurn {
  readonly currentSeat: Seat;
  readonly phase: TurnPhase;
  readonly round: number;
  readonly doublesStreak: number;
  readonly awardExtraRoll: boolean;
}

export interface GameSnapshot {
  readonly matchId: string;
  readonly phase: MatchPhase;
  readonly tick: number;
  readonly players: readonly SnapshotPlayer[];
  readonly spaces: readonly SnapshotSpace[];
  readonly turn: SnapshotTurn;
  readonly winnerId: PlayerId | null;
  readonly endReason: string;
  readonly configHash: string;
  /**
   * The per-recipient half: which player this frame is for, and what the engine
   * would accept from them right now.
   *
   * Optional because a snapshot is also a shared projection; the transport
   * populates it for the connection it is writing to. A client that does not
   * receive it falls back to deriving legality, which is what it had to do before.
   */
  readonly you?: ViewerState;
}

/** Recipient-specific state: identity plus the actions currently permitted. */
export interface ViewerState {
  readonly playerId: PlayerId;
  /** Action types the engine would accept from this player right now. */
  readonly legalActions: readonly ActionType[];
}

export type SpaceKindValue = SpaceKind;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isSafeInt(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v);
}

const PLAYER_STATUSES = ['active', 'eliminated', 'disconnected'] as const;
const TURN_PHASES = ['await_roll', 'await_buy_decision', 'turn_over'] as const;
/**
 * Match phases, mirroring the Go engine exactly.
 *
 * `interrupted` is the fourth terminal phase: it marks a match left active by a
 * process that died, which is never resumed. It was missing here while the engine
 * already emitted it, so an authoritative snapshot carrying it failed this very
 * type guard. `parity_test.go` now fails if the two lists ever diverge again.
 */
const MATCH_PHASES = ['lobby', 'playing', 'ended', 'interrupted'] as const;

export type MatchPhase = (typeof MATCH_PHASES)[number];
const SPACE_KINDS = ['go', 'property', 'tax', 'neutral'] as const;

export function isSnapshotPlayer(value: unknown): value is SnapshotPlayer {
  if (!isRecord(value)) return false;
  return (
    typeof value['playerId'] === 'string' &&
    typeof value['name'] === 'string' &&
    isSafeInt(value['seat']) &&
    isSafeInt(value['money']) &&
    PLAYER_STATUSES.includes(value['status'] as (typeof PLAYER_STATUSES)[number]) &&
    typeof value['ready'] === 'boolean' &&
    typeof value['isHost'] === 'boolean' &&
    isSafeInt(value['position'])
  );
}

export function isSnapshotSpace(value: unknown): value is SnapshotSpace {
  if (!isRecord(value)) return false;
  return (
    typeof value['id'] === 'string' &&
    SPACE_KINDS.includes(value['kind'] as (typeof SPACE_KINDS)[number]) &&
    typeof value['name'] === 'string' &&
    typeof value['group'] === 'string' &&
    isSafeInt(value['price']) &&
    isSafeInt(value['rent']) &&
    isSafeInt(value['amount']) &&
    typeof value['owned'] === 'boolean' &&
    typeof value['owner'] === 'string' &&
    isSafeInt(value['level'])
  );
}

export function isSnapshotTurn(value: unknown): value is SnapshotTurn {
  if (!isRecord(value)) return false;
  return (
    isSafeInt(value['currentSeat']) &&
    TURN_PHASES.includes(value['phase'] as (typeof TURN_PHASES)[number]) &&
    isSafeInt(value['round']) &&
    isSafeInt(value['doublesStreak']) &&
    typeof value['awardExtraRoll'] === 'boolean'
  );
}

export function isGameSnapshot(value: unknown): value is GameSnapshot {
  if (!isRecord(value)) return false;
  const winner = value['winnerId'];
  return (
    typeof value['matchId'] === 'string' &&
    MATCH_PHASES.includes(value['phase'] as (typeof MATCH_PHASES)[number]) &&
    isSafeInt(value['tick']) &&
    Array.isArray(value['players']) &&
    (value['players'] as unknown[]).every(isSnapshotPlayer) &&
    Array.isArray(value['spaces']) &&
    (value['spaces'] as unknown[]).every(isSnapshotSpace) &&
    isSnapshotTurn(value['turn']) &&
    (winner === null || typeof winner === 'string') &&
    typeof value['endReason'] === 'string' &&
    typeof value['configHash'] === 'string' &&
    // `you` is optional for forward compatibility: a server that does not send it
    // still produces a valid snapshot, and the client falls back to deriving.
    (value['you'] === undefined || isViewerState(value['you']))
  );
}

/**
 * Guard for the per-recipient block.
 *
 * `legalActions` is checked against the known action types rather than merely
 * checked as an array of strings: an unrecognised action is a contract drift
 * between the two implementations, and silently rendering an unknown button is how
 * that drift would reach a player.
 */
export function isViewerState(value: unknown): value is ViewerState {
  if (!isRecord(value)) return false;
  const actions = value['legalActions'];
  if (!Array.isArray(actions)) return false;
  if (typeof value['playerId'] !== 'string' || value['playerId'].length === 0) return false;
  return (actions as unknown[]).every(
    (a) => typeof a === 'string' && (ACTION_TYPES as readonly string[]).includes(a),
  );
}
