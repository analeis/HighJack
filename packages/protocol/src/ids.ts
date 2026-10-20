/**
 * Shared identifier types used across the protocol.
 *
 * Identifiers are opaque strings on the wire. Servers mint them; clients
 * treat them as tokens and never parse them.
 */

/** Opaque stable player identifier assigned by the server. */
export type PlayerId = string;

/** Zero-based seat order within a match. Deterministic turn ordering uses seat order. */
export type Seat = number;

/**
 * Amount of in-game currency ("chips").
 *
 * Always an integer. Fractional money is not representable by design:
 * it keeps economy arithmetic exact and deterministic across platforms.
 */
export type Money = number;
