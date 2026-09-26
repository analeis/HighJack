/**
 * Authoritative match state snapshots.
 *
 * Snapshots mirror `GameState` in server/internal/game. They are produced
 * exclusively by the server: clients render from them and never construct
 * them. Runtime guards validate the shapes a client receives; the engine
 * remains the sole authority over the values.
 */
import type { Money, PlayerId, Seat } from './ids.ts';
import type { BoardSpace, SpaceKind } from './config.ts';

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
    typeof value['configHash'] === 'string'
  );
}
