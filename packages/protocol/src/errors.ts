/**
 * Stable error codes for protocol-level failures.
 *
 * Error codes are part of the wire contract: never rename or repurpose an
 * existing code, only add new ones.
 */
export const ERROR_CODES = [
  'malformed_message',
  'unsupported_version',
  'unknown_message_type',
  'invalid_action',
  'not_permitted',
  'out_of_phase',
  'game_full',
  'already_started',
  'rate_limited',
  'not_supported',
  'internal_error',
] as const;

export type ErrorCode = (typeof ERROR_CODES)[number];

const ERROR_CODE_SET: ReadonlySet<string> = new Set(ERROR_CODES);

export function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === 'string' && ERROR_CODE_SET.has(value);
}

/** Structured error payload sent as a server `error` message. */
export interface ProtocolError {
  /** Stable machine-readable code (see ERROR_CODES). */
  readonly code: ErrorCode;
  /** Human-readable description safe to show to developers/operators. */
  readonly message: string;
  /** Optional structured context for programmatic handling. */
  readonly details?: Readonly<Record<string, unknown>>;
}

export function isProtocolError(value: unknown): value is ProtocolError {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  if (!isErrorCode(v['code'])) return false;
  if (typeof v['message'] !== 'string') return false;
  const { details } = v;
  if (details !== undefined && (typeof details !== 'object' || details === null)) return false;
  return true;
}
