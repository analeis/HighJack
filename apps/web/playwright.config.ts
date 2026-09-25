import { defineConfig, devices } from '@playwright/test';

/**
 * Browser verification for the HighJack website.
 * Serves the production build (astro preview) and runs route, navigation,
 * console-error, responsive, and accessibility checks.
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4321',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium-desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'chromium-mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    // Astro 7 `preview` always daemonizes: the spawned process exits
    // immediately after launching a background server, which Playwright
    // reports as "webServer exited early". A stale daemon (or its lock
    // file) from a previous run breaks subsequent runs the same way.
    // So: stop any stale daemon first (best effort, silent), start a fresh
    // one serving the just-built dist, then block on `tail -f /dev/null`
    // (portable, unlike `sleep infinity`) so Playwright has a foreground
    // process to manage while it polls the URL. The leftover daemon is
    // stopped by the next run's first step; steady state never exceeds one.
    command:
      'bun run preview stop >/dev/null 2>&1; bun run preview --host 127.0.0.1 --port 4321; tail -f /dev/null',
    url: 'http://127.0.0.1:4321',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
