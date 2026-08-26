import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const PAGES = ['/', '/game', '/features', '/rules', '/about', '/changelog'];

test.describe('accessibility', () => {
  for (const route of PAGES) {
    test(`${route} passes axe scans for serious/critical violations`, async ({ page }) => {
      await page.goto(route, { waitUntil: 'networkidle' });
      const results = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze();

      const serious = results.violations.filter(
        (v) => v.impact === 'serious' || v.impact === 'critical',
      );
      const summary = serious
        .map(
          (v) => `${v.id}(${v.impact}): ${v.nodes.length} nodes — ${v.nodes[0]?.target.join(' ')}`,
        )
        .join('\n');
      expect(serious, `${route}\n${summary}`).toEqual([]);
    });
  }

  test('/ has one h1 and a skip link target', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('h1')).toHaveCount(1);
    const main = page.locator('#main');
    await expect(main).toBeAttached();
  });

  test('images and icons carry accessible names where meaningful', async ({ page }) => {
    await page.goto('/');
    // The logo is decorative-adjacent but labeled; svg role=img must be named.
    const logo = page.locator('svg[role="img"]');
    const count = await logo.count();
    for (let i = 0; i < count; i++) {
      await expect(logo.nth(i)).toHaveAccessibleName(/.+/);
    }
  });
});
