/**
 * Message envelopes: the unit of transport between client and server.
 *
 * Every WebSocket text frame carries exactly one JSON message matching one
 * of the discriminated unions below. The `v` header carries the protocol
 * major version so a server can reject incompatible clients with a single
 * structured error before any payload parsing.
 *
 * Transport notes:
 * - JSON only in v0.1 (see docs/protocol/OVERVIEW.md for replacement criteria).
 * - One message per frame; no batching, no binary frames yet.
 */
import type { GameAction } from './actions.ts';
import { isGameAction } from './actions.ts';
import type { ProtocolError } from './errors.ts';
import { isProtocolError } from './errors.ts';
import type { GameEvent } from './events.ts';
import { isGameEvent } from './events.ts';
import { PROTOCOL_MAJOR_VERSION } from './version.ts';

/** Protocol major version stamp present on every envelope. */
export type ProtocolMajor = typeof PROTOCOL_MAJOR_VERSION;

interface EnvelopeBase {
  /** Protocol major version. Messages with an unknown `v` are rejected. */
  readonly v: ProtocolMajor;
}

// ---- client → server -------------------------------------------------------

export interface HelloMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'hello';
  /** Short client identifier, e.g. "web/0.1.0". */
  readonly client: string;
}

export interface PingMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'ping';
  readonly nonce: number;
}

export interface ActionMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'action';
  /** Per-connection monotonically increasing sequence number. */
  readonly seq: number;
  readonly action: GameAction;
}

export type ClientMessage = HelloMessage | PingMessage | ActionMessage;

// ---- server → client -------------------------------------------------------

export interface WelcomeMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'welcome';
  /** Server-assigned connection session id. */
  readonly session: string;
  readonly protocolVersion: string;
  /** Server heartbeat cadence hint in milliseconds (0 = none). */
  readonly heartbeatMs: number;
}

export interface PongMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'pong';
  readonly nonce: number;
}

export interface EventMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'event';
  /** Engine tick that produced the event; defines total order. */
  readonly tick: number;
  readonly event: GameEvent;
}

export interface ErrorMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'error';
  readonly error: ProtocolError;
  /** `seq` of the client action that triggered the error, when applicable. */
  readonly ackSeq?: number | undefined;
}

export type ServerMessage = WelcomeMessage | PongMessage | EventMessage | ErrorMessage;

// ---- runtime guards --------------------------------------------------------

function isEnvelope(value: unknown): value is Record<string, unknown> {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    (value as Record<string, unknown>)['v'] === PROTOCOL_MAJOR_VERSION
  );
}

export function isHelloMessage(value: unknown): value is HelloMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return v['type'] === 'hello' && typeof v['client'] === 'string' && v['client'].length > 0;
}

export function isPingMessage(value: unknown): value is PingMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return v['type'] === 'ping' && typeof v['nonce'] === 'number' && Number.isSafeInteger(v['nonce']);
}

export function isActionMessage(value: unknown): value is ActionMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    v['type'] === 'action' &&
    typeof v['seq'] === 'number' &&
    Number.isSafeInteger(v['seq']) &&
    isGameAction(v['action'])
  );
}

export function isClientMessage(value: unknown): value is ClientMessage {
  return isHelloMessage(value) || isPingMessage(value) || isActionMessage(value);
}

export function isWelcomeMessage(value: unknown): value is WelcomeMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    v['type'] === 'welcome' &&
    typeof v['session'] === 'string' &&
    typeof v['protocolVersion'] === 'string' &&
    typeof v['heartbeatMs'] === 'number'
  );
}

export function isPongMessage(value: unknown): value is PongMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return v['type'] === 'pong' && typeof v['nonce'] === 'number';
}

export function isEventMessage(value: unknown): value is EventMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return v['type'] === 'event' && typeof v['tick'] === 'number' && isGameEvent(v['event']);
}

export function isErrorMessage(value: unknown): value is ErrorMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  if (v['type'] !== 'error' || !isProtocolError(v['error'])) return false;
  const ackSeq = v['ackSeq'];
  return ackSeq === undefined || typeof ackSeq === 'number';
}

export function isServerMessage(value: unknown): value is ServerMessage {
  return (
    isWelcomeMessage(value) ||
    isPongMessage(value) ||
    isEventMessage(value) ||
    isErrorMessage(value)
  );
}
