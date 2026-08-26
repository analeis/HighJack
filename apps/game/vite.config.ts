import { defineConfig } from 'vitest/config';
import solid from 'vite-plugin-solid';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [tailwindcss(), solid()],
  build: {
    target: 'es2022',
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
