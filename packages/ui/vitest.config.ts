import { defineConfig } from 'vitest/config';
import solid from 'vite-plugin-solid';

export default defineConfig({
  plugins: [solid()],
  test: {
    include: ['test/**/*.test.tsx'],
    environment: 'jsdom',
    globals: true,
    setupFiles: ['test/setup.ts'],
  },
  resolve: {
    conditions: ['development', 'browser'],
  },
});
