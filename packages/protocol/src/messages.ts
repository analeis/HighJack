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
import type { GameConfig } from './config.ts';
import type { GameSnapshot } from './state.ts';
import { isGameSnapshot } from './state.ts';
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
  /** Match to bind this connection to (omit for handshake-only). */
  readonly matchId?: string | undefined;
  /** Join token minted by POST /matches/{id}/players. */
  readonly token?: string | undefined;
  /** Last engine tick the client has applied; drives catch-up. */
  readonly resumeFromTick?: number | undefined;
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

export interface SnapshotMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'snapshot';
  /** Full authoritative state; clients must render from this, never guess. */
  readonly snapshot: GameSnapshot;
  /** Match configuration the snapshot runs under (board data included). */
  readonly config: GameConfig;
  /** Next action `seq` the server expects from this player. */
  readonly nextSeq: number;
  /** True when the client cursor was too old and no catch-up was attempted. */
  readonly resync: boolean;
}

export interface CatchupMessage extends EnvelopeBase {
  readonly v: ProtocolMajor;
  readonly type: 'catchup';
  /** Missed events with their engine ticks, in causal order. */
  readonly events: readonly { readonly tick: number; readonly event: GameEvent }[];
  /** Engine tick the catch-up reaches (client cursor becomes this). */
  readonly cursor: number;
}

export type ServerMessage =
  WelcomeMessage | PongMessage | EventMessage | ErrorMessage | SnapshotMessage | CatchupMessage;

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
  if (v['type'] !== 'hello' || typeof v['client'] !== 'string' || v['client'].length === 0) {
    return false;
  }
  for (const key of ['matchId', 'token'] as const) {
    if (v[key] !== undefined && typeof v[key] !== 'string') return false;
  }
  const resume = v['resumeFromTick'];
  if (resume !== undefined && (typeof resume !== 'number' || !Number.isSafeInteger(resume))) {
    return false;
  }
  return true;
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
    isErrorMessage(value) ||
    isSnapshotMessage(value) ||
    isCatchupMessage(value)
  );
}

export function isSnapshotMessage(value: unknown): value is SnapshotMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    v['type'] === 'snapshot' &&
    isGameSnapshot(v['snapshot']) &&
    typeof v['config'] === 'object' &&
    v['config'] !== null &&
    typeof v['nextSeq'] === 'number' &&
    Number.isSafeInteger(v['nextSeq']) &&
    typeof v['resync'] === 'boolean'
  );
}

export function isCatchupMessage(value: unknown): value is CatchupMessage {
  if (!isEnvelope(value)) return false;
  const v = value as Record<string, unknown>;
  if (v['type'] !== 'catchup' || !Array.isArray(v['events'])) return false;
  for (const item of v['events'] as unknown[]) {
    if (typeof item !== 'object' || item === null) return false;
    const rec = item as Record<string, unknown>;
    if (typeof rec['tick'] !== 'number' || !Number.isSafeInteger(rec['tick'])) return false;
    if (!isGameEvent(rec['event'])) return false;
  }
  return typeof v['cursor'] === 'number' && Number.isSafeInteger(v['cursor']);
}
