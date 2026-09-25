/**
 * HighJack protocol versioning.
 *
 * PROTOCOL_VERSION follows semver. The major component is the wire
 * compatibility contract: clients and servers with the same major version
 * can communicate. Minor/patch bumps must be backwards compatible.
 *
 * SCHEMA_VERSION is a monotonically increasing integer identifying the
 * revision of individual message payload schemas. It is embedded in
 * fixtures and used by compatibility tests on both the TypeScript and Go
 * sides of the boundary.
 */

export const PROTOCOL_VERSION = '1.1.0';

/** Integer protocol major version carried by every message envelope (`v`). */
export const PROTOCOL_MAJOR_VERSION = 1;

/** Revision of message/action/event payload schemas shared via fixtures. */
export const SCHEMA_VERSION = 1;

export interface VersionInfo {
  readonly protocolVersion: string;
  readonly schemaVersion: number;
}

export const VERSION_INFO: VersionInfo = {
  protocolVersion: PROTOCOL_VERSION,
  schemaVersion: SCHEMA_VERSION,
};
