/**
 * Game events: facts emitted by the authoritative engine.
 *
 * Events are the *only* output of a state transition besides the new state.
 * They describe what changed, never commands. Every event carries the
 * engine tick at which it was produced; events do not carry wall-clock
 * time so that event logs remain deterministic and replayable.
 */
import type { Money, PlayerId, Seat } from './ids.ts';

export const EVENT_TYPES = [
  'player_joined',
  'player_left',
  'player_ready_changed',
  'game_started',
  'player_eliminated',
  'game_ended',
] as const;

export type EventType = (typeof EVENT_TYPES)[number];

interface EventBase {
  readonly type: EventType;
}

export interface PlayerJoinedEvent extends EventBase {
  readonly type: 'player_joined';
  readonly playerId: PlayerId;
  readonly name: string;
  readonly seat: Seat;
  readonly startingMoney: Money;
}

export interface PlayerLeftEvent extends EventBase {
  readonly type: 'player_left';
  readonly playerId: PlayerId;
  /** "voluntary" or "disconnected"; extensible via new codes only. */
  readonly reason: 'voluntary' | 'disconnected';
}

export interface PlayerReadyChangedEvent extends EventBase {
  readonly type: 'player_ready_changed';
  readonly playerId: PlayerId;
  readonly ready: boolean;
}

export interface GameStartedEvent extends EventBase {
  readonly type: 'game_started';
  /** Hash of the canonical serialized GameConfig the match runs under. */
  readonly configHash: string;
  /** Deterministic RNG seed for the match (hex-encoded). */
  readonly seed: string;
}

export interface PlayerEliminatedEvent extends EventBase {
  readonly type: 'player_eliminated';
  readonly playerId: PlayerId;
  readonly cause: string;
}

export interface GameEndedEvent extends EventBase {
  readonly type: 'game_ended';
  readonly winnerId: PlayerId | null;
  /** Machine-readable victory condition that decided the match. */
  readonly reason: string;
}

export type GameEvent =
  | PlayerJoinedEvent
  | PlayerLeftEvent
  | PlayerReadyChangedEvent
  | GameStartedEvent
  | PlayerEliminatedEvent
  | GameEndedEvent;

// ---- runtime guards --------------------------------------------------------

const EVENT_TYPE_SET: ReadonlySet<string> = new Set(EVENT_TYPES);

export function isEventType(value: unknown): value is EventType {
  return typeof value === 'string' && EVENT_TYPE_SET.has(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasString(v: Record<string, unknown>, key: string): boolean {
  return typeof v[key] === 'string';
}

export function isPlayerJoinedEvent(value: unknown): value is PlayerJoinedEvent {
  if (!isRecord(value) || value['type'] !== 'player_joined') return false;
  return (
    hasString(value, 'playerId') &&
    hasString(value, 'name') &&
    typeof value['seat'] === 'number' &&
    Number.isSafeInteger(value['seat']) &&
    typeof value['startingMoney'] === 'number'
  );
}

export function isPlayerLeftEvent(value: unknown): value is PlayerLeftEvent {
  if (!isRecord(value) || value['type'] !== 'player_left') return false;
  return (
    hasString(value, 'playerId') &&
    (value['reason'] === 'voluntary' || value['reason'] === 'disconnected')
  );
}

export function isPlayerReadyChangedEvent(value: unknown): value is PlayerReadyChangedEvent {
  if (!isRecord(value) || value['type'] !== 'player_ready_changed') return false;
  return hasString(value, 'playerId') && typeof value['ready'] === 'boolean';
}

export function isGameStartedEvent(value: unknown): value is GameStartedEvent {
  if (!isRecord(value) || value['type'] !== 'game_started') return false;
  return hasString(value, 'configHash') && hasString(value, 'seed');
}

export function isPlayerEliminatedEvent(value: unknown): value is PlayerEliminatedEvent {
  if (!isRecord(value) || value['type'] !== 'player_eliminated') return false;
  return hasString(value, 'playerId') && hasString(value, 'cause');
}

export function isGameEndedEvent(value: unknown): value is GameEndedEvent {
  if (!isRecord(value) || value['type'] !== 'game_ended') return false;
  const winner = value['winnerId'];
  const winnerOk = winner === null || typeof winner === 'string';
  return winnerOk && hasString(value, 'reason');
}

export function isGameEvent(value: unknown): value is GameEvent {
  if (!isRecord(value)) return false;
  switch (value['type']) {
    case 'player_joined':
      return isPlayerJoinedEvent(value);
    case 'player_left':
      return isPlayerLeftEvent(value);
    case 'player_ready_changed':
      return isPlayerReadyChangedEvent(value);
    case 'game_started':
      return isGameStartedEvent(value);
    case 'player_eliminated':
      return isPlayerEliminatedEvent(value);
    case 'game_ended':
      return isGameEndedEvent(value);
    default:
      return false;
  }
}
