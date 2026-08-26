/**
 * Shared identifier types used across the protocol.
 *
 * Identifiers are opaque strings on the wire. Servers mint them; clients
 * treat them as tokens and never parse them.
 */

/** Opaque stable player identifier assigned by the server. */
export type PlayerId = string;

/** Opaque match/session identifier assigned by the server. */
export type GameId = string;

/** Zero-based seat order within a match. Deterministic turn ordering uses seat order. */
export type Seat = number;

/**
 * Amount of in-game currency ("chips").
 *
 * Always an integer. Fractional money is not representable by design:
 * it keeps economy arithmetic exact and deterministic across platforms.
 */
export type Money = number;

export function isMoney(value: unknown): value is Money {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

const ID_MAX_LENGTH = 128;

export function isPlayerId(value: unknown): value is PlayerId {
  return typeof value === 'string' && value.length > 0 && value.length <= ID_MAX_LENGTH;
}
