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
  'dice_rolled',
  'property_bought',
  'buy_declined',
  'rent_paid',
  'bank_transfer',
  'player_bankrupt',
  'turn_advanced',
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

export interface DiceRolledEvent extends EventBase {
  readonly type: 'dice_rolled';
  readonly playerId: PlayerId;
  readonly die1: number;
  readonly die2: number;
  readonly fromSpace: string;
  readonly toSpace: string;
  readonly passedGo: boolean;
}

export interface PropertyBoughtEvent extends EventBase {
  readonly type: 'property_bought';
  readonly playerId: PlayerId;
  readonly spaceId: string;
  readonly price: Money;
}

export interface BuyDeclinedEvent extends EventBase {
  readonly type: 'buy_declined';
  readonly playerId: PlayerId;
  readonly spaceId: string;
}

export interface RentPaidEvent extends EventBase {
  readonly type: 'rent_paid';
  readonly fromPlayerId: PlayerId;
  readonly toPlayerId: PlayerId;
  readonly spaceId: string;
  readonly amount: Money;
}

export interface BankTransferEvent extends EventBase {
  readonly type: 'bank_transfer';
  readonly playerId: PlayerId;
  readonly amount: Money;
  readonly direction: 'to_player' | 'to_bank';
  /** "pass_go" | "land_go" | "tax"; extensible via new codes only. */
  readonly reason: string;
}

export interface PlayerBankruptEvent extends EventBase {
  readonly type: 'player_bankrupt';
  readonly playerId: PlayerId;
  readonly cause: string;
  readonly creditorId?: string | undefined;
  readonly amountOwed: Money;
}

export interface TurnAdvancedEvent extends EventBase {
  readonly type: 'turn_advanced';
  readonly seat: Seat;
  readonly round: number;
}

export type GameEvent =
  | PlayerJoinedEvent
  | PlayerLeftEvent
  | PlayerReadyChangedEvent
  | GameStartedEvent
  | PlayerEliminatedEvent
  | GameEndedEvent
  | DiceRolledEvent
  | PropertyBoughtEvent
  | BuyDeclinedEvent
  | RentPaidEvent
  | BankTransferEvent
  | PlayerBankruptEvent
  | TurnAdvancedEvent;

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

function isSafeInt(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v);
}

export function isDiceRolledEvent(value: unknown): value is DiceRolledEvent {
  if (!isRecord(value) || value['type'] !== 'dice_rolled') return false;
  return (
    hasString(value, 'playerId') &&
    isSafeInt(value['die1']) &&
    isSafeInt(value['die2']) &&
    hasString(value, 'fromSpace') &&
    hasString(value, 'toSpace') &&
    typeof value['passedGo'] === 'boolean'
  );
}

export function isPropertyBoughtEvent(value: unknown): value is PropertyBoughtEvent {
  if (!isRecord(value) || value['type'] !== 'property_bought') return false;
  return hasString(value, 'playerId') && hasString(value, 'spaceId') && isSafeInt(value['price']);
}

export function isBuyDeclinedEvent(value: unknown): value is BuyDeclinedEvent {
  if (!isRecord(value) || value['type'] !== 'buy_declined') return false;
  return hasString(value, 'playerId') && hasString(value, 'spaceId');
}

export function isRentPaidEvent(value: unknown): value is RentPaidEvent {
  if (!isRecord(value) || value['type'] !== 'rent_paid') return false;
  return (
    hasString(value, 'fromPlayerId') &&
    hasString(value, 'toPlayerId') &&
    hasString(value, 'spaceId') &&
    isSafeInt(value['amount'])
  );
}

export function isBankTransferEvent(value: unknown): value is BankTransferEvent {
  if (!isRecord(value) || value['type'] !== 'bank_transfer') return false;
  return (
    hasString(value, 'playerId') &&
    isSafeInt(value['amount']) &&
    (value['direction'] === 'to_player' || value['direction'] === 'to_bank') &&
    hasString(value, 'reason')
  );
}

export function isPlayerBankruptEvent(value: unknown): value is PlayerBankruptEvent {
  if (!isRecord(value) || value['type'] !== 'player_bankrupt') return false;
  const creditor = value['creditorId'];
  return (
    hasString(value, 'playerId') &&
    hasString(value, 'cause') &&
    (creditor === undefined || typeof creditor === 'string') &&
    isSafeInt(value['amountOwed'])
  );
}

export function isTurnAdvancedEvent(value: unknown): value is TurnAdvancedEvent {
  if (!isRecord(value) || value['type'] !== 'turn_advanced') return false;
  return isSafeInt(value['seat']) && isSafeInt(value['round']);
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
    case 'dice_rolled':
      return isDiceRolledEvent(value);
    case 'property_bought':
      return isPropertyBoughtEvent(value);
    case 'buy_declined':
      return isBuyDeclinedEvent(value);
    case 'rent_paid':
      return isRentPaidEvent(value);
    case 'bank_transfer':
      return isBankTransferEvent(value);
    case 'player_bankrupt':
      return isPlayerBankruptEvent(value);
    case 'turn_advanced':
      return isTurnAdvancedEvent(value);
    default:
      return false;
  }
}
