/// <reference types="vite/client" />

/**
 * The release this client was built from.
 *
 * Injected at build time from the repository's VERSION file, so the client
 * cannot name a release that does not exist. It was a literal for a long time,
 * and it was two releases stale.
 */
declare const __APP_VERSION__: string;
