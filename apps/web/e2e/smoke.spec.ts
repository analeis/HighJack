import { expect, test } from '@playwright/test';

const ROUTES = ['/', '/game', '/features', '/rules', '/about', '/changelog'];

test.describe('routes render', () => {
  for (const route of ROUTES) {
    test(`${route} renders its heading and main landmark`, async ({ page }) => {
      const errors: string[] = [];
      page.on('console', (msg) => {
        if (msg.type() === 'error') errors.push(msg.text());
      });
      page.on('pageerror', (err) => errors.push(String(err)));

      await page.goto(route, { waitUntil: 'networkidle' });
      await expect(page.locator('main')).toBeVisible();
      await expect(page.locator('h1').first()).toBeVisible();

      // No catastrophic layout failure: page must have real height.
      const height = await page.evaluate(() => document.body.scrollHeight);
      expect(height).toBeGreaterThan(400);

      expect(errors, `console errors on ${route}`).toEqual([]);
    });
  }
});

test.describe('navigation', () => {
  test('desktop header navigates between routes', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-desktop', 'desktop-only');
    await page.goto('/');
    await page
      .getByRole('navigation', { name: 'Primary' })
      .getByRole('link', { name: 'Features' })
      .click();
    await expect(page).toHaveURL(/\/features$/);
    await expect(page.getByRole('heading', { name: /Systems & features/i })).toBeVisible();
  });

  test('current route is marked with aria-current', async ({ page }) => {
    await page.goto('/about');
    const current = page.locator('[aria-current="page"]');
    await expect(current).toHaveText(/About/);
  });

  test('mobile drawer opens and navigates', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-mobile', 'mobile-only');
    await page.goto('/');
    await page.getByRole('button', { name: 'Open menu' }).click();
    const closeToggle = page.getByRole('button', { name: 'Close menu' });
    await expect(closeToggle).toHaveAttribute('aria-expanded', 'true');
    await page
      .getByRole('navigation', { name: 'Mobile' })
      .getByRole('link', { name: 'Rules' })
      .click();
    await expect(page).toHaveURL(/\/rules$/);
    // Drawer closed itself after navigation.
    await expect(page.getByRole('navigation', { name: 'Mobile' })).toBeHidden();
  });

  test('skip link appears on focus', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-desktop', 'desktop-only');
    await page.goto('/');
    await page.keyboard.press('Tab');
    await expect(page.getByRole('link', { name: 'Skip to main content' })).toBeFocused();
  });
});

test.describe('key interactive elements', () => {
  test('configuration preview island hydrates and validates', async ({ page }) => {
    await page.goto('/features', { waitUntil: 'networkidle' });
    const poker = page.getByRole('switch', { name: 'Poker nights' });
    await expect(poker).toBeVisible();
    // Invalid state first: poker on, gambling master switch off.
    await poker.click();
    const preview = page.locator('pre[aria-label="GameConfig JSON preview"]');
    await expect(page.getByRole('alert')).toContainText('gambling');
    // Master switch on → valid again, alert disappears.
    await page.getByRole('switch', { name: 'Gambling tables (master switch)' }).click();
    await expect(page.getByRole('alert')).toBeHidden();
    await expect(preview).toContainText('"poker": true');
  });

  test('hero CTAs point at real routes', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Peek at the game' })).toHaveAttribute(
      'href',
      '/game',
    );
    await expect(page.getByRole('link', { name: 'Explore systems' })).toHaveAttribute(
      'href',
      '/features',
    );
  });
});

test.describe('responsive smoke', () => {
  test('no horizontal overflow at common widths', async ({ page }, testInfo) => {
    for (const route of ROUTES.slice(0, 3)) {
      await page.goto(route);
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(
        overflow,
        `horizontal overflow on ${route} (${testInfo.project.name})`,
      ).toBeLessThanOrEqual(2);
    }
  });
});
