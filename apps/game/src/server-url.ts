/**
 * Where the game server is.
 *
 * The server is the authority; its address is deployment configuration
 * (`VITE_HIGHJACK_SERVER`) with a same-host default that matches the port the
 * server binds in development and in the certification harness.
 *
 * It lives here rather than in `App.tsx` because two modules need it: the
 * application shell, which opens the socket, and the lobby, which shows the
 * player which server they are about to join.
 */
export const SERVER_PORT = import.meta.env['VITE_HIGHJACK_PORT'] ?? '8080';

export const SERVER_URL =
  import.meta.env['VITE_HIGHJACK_SERVER'] ??
  (typeof window === 'undefined'
    ? `http://127.0.0.1:${SERVER_PORT}`
    : `${window.location.protocol}//${window.location.hostname}:${SERVER_PORT}`);

export const WS_URL = `${SERVER_URL.replace(/^http/, 'ws')}/ws`;
