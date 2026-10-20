import { readFileSync } from 'node:fs';
import { defineConfig } from 'vitest/config';
import solid from 'vite-plugin-solid';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [tailwindcss(), solid()],
  build: {
    // Matches the type-checker's target. The two have to agree: a `target` in
    // the tsconfig that is newer than the one the bundle is built for would
    // type-check against APIs the shipped code cannot rely on.
    target: 'es2023',
  },
  // The client says which release it is, and it reads that from the repository's
  // VERSION file rather than from a literal that has to be remembered. The
  // literal was two releases stale, and the manifests around it named a
  // different version again.
  define: {
    __APP_VERSION__: JSON.stringify(
      readFileSync(new URL('../../VERSION', import.meta.url), 'utf8').trim(),
    ),
  },
  test: {
    include: ['test/**/*.test.ts'],
    environment: 'node',
    globals: true,
  },
  resolve: {
    conditions: ['development', 'browser'],
  },
});
