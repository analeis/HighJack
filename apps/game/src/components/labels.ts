import type { ConnectionStatus } from '../net/connection.ts';

/**
 * Words the player sees.
 *
 * These live in one place so the same fact is named the same way in the header,
 * the banner, the live region and the screen-reader board. The wire values
 * underneath are engine vocabulary and must never leak into the interface.
 */

/** A human name for a connection state. */
export function statusLabel(status: ConnectionStatus): string {
  switch (status) {
    case 'connected':
      return 'Connected';
    case 'connecting':
      return 'Connecting…';
    case 'handshaking':
      return 'Taking your seat…';
    case 'reconnecting':
      return 'Reconnecting…';
    case 'disconnected':
      return 'Offline';
    case 'error':
      return 'Connection lost';
    case 'idle':
      return 'Not connected';
    default:
      return 'Not connected';
  }
}

/** A human name for a player's state at the table. */
export function playerStatusLabel(status: string): string {
  switch (status) {
    case 'active':
      return 'Playing';
    case 'eliminated':
      return 'Out';
    case 'disconnected':
      return 'Away';
    default:
      return status;
  }
}

/** A human name for the reason a match finished. */
export function endReasonLabel(reason: string): string {
  switch (reason) {
    case 'last_standing':
      return 'last player standing';
    case 'target_wealth':
      return 'target wealth reached';
    case 'round_limit':
      return 'round limit reached';
    case 'insufficient_players':
      return 'not enough players';
    default:
      return reason;
  }
}
