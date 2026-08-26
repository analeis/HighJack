/**
 * Game actions: client-intended state transitions.
 *
 * An action is a *request*; it never mutates anything by itself. The
 * authoritative engine validates the action against current state and, if
 * valid, performs exactly one transition and emits events.
 *
 * Actions deliberately do NOT carry a player id: the server derives the
 * acting player from the authenticated connection, so a client cannot
 * spoof another player.
 */
import type { Money } from './ids.ts';

export const ACTION_TYPES = ['player_join', 'player_leave', 'player_ready', 'game_start'] as const;

export type ActionType = (typeof ACTION_TYPES)[number];

interface ActionBase {
  readonly type: ActionType;
}

export interface PlayerJoinAction extends ActionBase {
  readonly type: 'player_join';
  /** Requested display name, 1..32 characters after trimming. Server may sanitize. */
  readonly displayName: string;
}

export interface PlayerLeaveAction extends ActionBase {
  readonly type: 'player_leave';
}

export interface PlayerReadyAction extends ActionBase {
  readonly type: 'player_ready';
  readonly ready: boolean;
}

export interface GameStartAction extends ActionBase {
  readonly type: 'game_start';
}

export type GameAction = PlayerJoinAction | PlayerLeaveAction | PlayerReadyAction | GameStartAction;

// ---- runtime guards --------------------------------------------------------

const ACTION_TYPE_SET: ReadonlySet<string> = new Set(ACTION_TYPES);

export function isActionType(value: unknown): value is ActionType {
  return typeof value === 'string' && ACTION_TYPE_SET.has(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function isPlayerJoinAction(value: unknown): value is PlayerJoinAction {
  if (!isRecord(value)) return false;
  if (value['type'] !== 'player_join') return false;
  if (typeof value['displayName'] !== 'string') return false;
  return true;
}

export function isPlayerLeaveAction(value: unknown): value is PlayerLeaveAction {
  return isRecord(value) && value['type'] === 'player_leave';
}

export function isPlayerReadyAction(value: unknown): value is PlayerReadyAction {
  if (!isRecord(value)) return false;
  return value['type'] === 'player_ready' && typeof value['ready'] === 'boolean';
}

export function isGameStartAction(value: unknown): value is GameStartAction {
  return isRecord(value) && value['type'] === 'game_start';
}

export function isGameAction(value: unknown): value is GameAction {
  if (!isRecord(value)) return false;
  switch (value['type']) {
    case 'player_join':
      return isPlayerJoinAction(value);
    case 'player_leave':
      return isPlayerLeaveAction(value);
    case 'player_ready':
      return isPlayerReadyAction(value);
    case 'game_start':
      return isGameStartAction(value);
    default:
      return false;
  }
}

/**
 * Example of an economy-shaped amount field used by future actions.
 * Exported so tests can pin down the integer-money contract today.
 */
export function isValidWager(value: unknown): value is Money {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}
