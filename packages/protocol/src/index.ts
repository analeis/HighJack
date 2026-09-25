/**
 * @highjack/protocol — the HighJack client/server contract.
 *
 * This package is transport-agnostic: it defines message shapes, actions,
 * events, errors, and versioning. It must never import HTTP, WebSocket,
 * DOM, or server implementation details.
 */
export * from './version.ts';
export * from './ids.ts';
export * from './errors.ts';
export * from './actions.ts';
export * from './events.ts';
export * from './messages.ts';
export * from './config.ts';
export * from './state.ts';
