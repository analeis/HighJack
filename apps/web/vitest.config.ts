import { defineConfig } from 'vitest/config';

/**
 * The website's unit-test configuration.
 *
 * The `e2e/` directory holds Playwright specs, not Vitest ones. Without this
 * configuration `vitest run` collects them, tries to execute a Playwright test
 * as a unit test, and fails — so a `bun run test` at the root that included
 * this workspace would report a failure that has nothing to do with the code.
 *
 * `include` is deliberately narrow: a unit test belongs in `test/`, and an
 * end-to-end spec belongs in `e2e/`.
 */
export default defineConfig({
  test: {
    include: ['test/**/*.test.ts'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**', '.astro/**'],
  },
});
