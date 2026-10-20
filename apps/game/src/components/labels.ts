import type { ConnectionStatus } from '../net/connection.ts';

/**
 * Labels the interface shows.
 *
 * They live in one place so the same word is used for the same thing wherever
 * it appears — the connection state is named identically in the header and in
 * the banner.
 */

/** A human name for a connection state. */
export function statusLabel(status: ConnectionStatus): string {
  switch (status) {
    case 'connected':
      return 'connected';
    case 'connecting':
      return 'connecting…';
    case 'handshaking':
      return 'joining…';
    case 'reconnecting':
      return 'reconnecting…';
    case 'disconnected':
      return 'disconnected';
    case 'error':
      return 'connection error';
    default:
      return 'not connected';
  }
}
