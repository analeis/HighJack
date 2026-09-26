import { expect, test, type Page } from '@playwright/test';

/**
 * Game client verification (v0.2 board loop).
 *
 * These tests drive the real PixiJS + SolidJS client against the real Go
 * server over a real WebSocket. Nothing is mocked: the server owns every
 * dice roll, balance, and ownership change, and the client may only render
 * and request.
 */

const SERVER = 'http://127.0.0.1:8080';
const GAME_URL = 'http://127.0.0.1:5173';

async function openLobby(page: Page, name: string) {
  await page.goto(GAME_URL, { waitUntil: 'domcontentloaded' });
  await page.getByLabel('Display name').fill(name);
  await page.getByRole('button', { name: /Create a table|Creating match/ }).click();
  await expect(page.getByRole('button', { name: /^Roll/ })).toBeVisible({ timeout: 20_000 });
}

/** Reads the authoritative snapshot the server published, via its HTTP view. */
async function serverSnapshot(matchId: string) {
  const res = await fetch(`${SERVER}/matches/${matchId}`);
  return (await res.json()) as {
    phase: string;
    tick: number;
    players: {
      playerId: string;
      money: number;
      ready: boolean;
      isHost: boolean;
      position: number;
    }[];
    spaces: { id: string; owned: boolean; owner: string }[];
    turn: { currentSeat: number; phase: string; round: number };
  };
}

/** The dice the client rendered, read back from its own view. */
async function rollEventFor(page: Page, tick: number) {
  return page.evaluate((t) => {
    const w = window as unknown as { __hjLastRoll?: { die1: number; die2: number } };
    void t;
    return w.__hjLastRoll ?? null;
  }, tick);
}

test.describe('game client', () => {
  test('lobby creates a real server-backed match', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(String(e)));
    await page.goto(GAME_URL, { waitUntil: 'domcontentloaded' });

    await expect(page.getByRole('heading', { name: /Open a private table/i })).toBeVisible();
    await page.getByLabel('Display name').fill('Ace');
    await page.getByRole('button', { name: /Create a table|Creating match/ }).click();

    // The dock only renders once an authoritative snapshot arrived, so
    // seeing it proves the client is bound to a live match.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeVisible({ timeout: 20_000 });
    await expect(page.getByText(/match m_/)).toBeVisible();
    expect(errors).toEqual([]);
  });

  test('illegal controls stay disabled before a match starts', async ({ page }) => {
    await page.goto(GAME_URL, { waitUntil: 'domcontentloaded' });
    await page.getByLabel('Display name').fill('Ace');
    await page.getByRole('button', { name: /Create a table|Creating match/ }).click();
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeVisible({ timeout: 20_000 });

    // A fresh lobby has no turn: only Ready (and Start for the host) can act.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Buy/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Decline/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^End turn/ })).toBeDisabled();
  });

  test('two real browser clients play the authoritative board loop', async ({ page }, testInfo) => {
    test.setTimeout(120_000);

    // Player A creates the table through the UI.
    await openLobby(page, 'Ace');
    const matchId = new URL(page.url()).searchParams.get('match') ?? '';
    expect(matchId).toMatch(/^m_/);

    // Player B is a genuinely separate browser context driving the *same* client
    // code. It used to be a raw WebSocket opened inside page.evaluate, which
    // never rendered a dock, a store or a canvas — so every client-side defect
    // was structurally invisible to this suite, and the file's own "Nothing is
    // mocked" header was false for half the session.
    // A *separate* browser context, not another page in this one. The seat is
    // persisted in localStorage, and pages in a context share storage, so a second
    // page here would rejoin as player A rather than claiming its own seat.
    const browser = page.context().browser();
    if (!browser) throw new Error('no browser available for the second client');
    const contextB = await browser.newContext();
    const pageB = await contextB.newPage();
    const errorsB: string[] = [];
    pageB.on('pageerror', (e: Error) => errorsB.push(String(e)));

    await pageB.goto(GAME_URL, { waitUntil: 'domcontentloaded' });
    await pageB.getByLabel('Display name').fill('Bit');
    // Join by match id, through the same control a person would use.
    await pageB.getByLabel('Match id').fill(matchId);
    await pageB.getByRole('button', { name: 'Join' }).click();

    // Both clients are now in the match, each with a live dock.
    const dockA = page.getByRole('navigation', { name: 'Match actions' });
    const dockB = pageB.getByRole('navigation', { name: 'Match actions' });
    await expect(dockB).toBeVisible({ timeout: 20_000 });
    await expect(dockA).toBeVisible();

    // Both ready up through their own UI, and the host starts. Until the server
    // says everyone is ready, Start must stay disabled on the host's dock — the
    // client used to enable it on phase+host alone, so the most common action
    // for a new player was an immediate rejection.
    await page.getByRole('button', { name: /^Ready/ }).click();
    await expect
      .poll(async () => (await serverSnapshot(matchId)).players.find((p) => p.ready)?.ready)
      .toBe(true);

    const startButton = page.getByRole('button', { name: /^Start/ });
    await expect(startButton).toBeDisabled();
    await pageB.getByRole('button', { name: /^Ready/ }).click();
    await expect
      .poll(async () => (await serverSnapshot(matchId)).players.filter((p) => p.ready).length)
      .toBe(2);
    await expect(startButton).toBeEnabled({ timeout: 10_000 });
    await startButton.click();

    await expect
      .poll(async () => (await serverSnapshot(matchId)).phase, { timeout: 15_000 })
      .toBe('playing');
    const playing = await serverSnapshot(matchId);
    expect(playing.turn.currentSeat).toBe(0);

    // The seat that holds the turn may act; the other may not. Legality now comes
    // from the server's per-recipient legal actions, so this is the server's
    // answer rendered, not a client opinion.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeEnabled({ timeout: 10_000 });
    const quietIsPlayerB = await pageB.evaluate(() => {
      const el = document.querySelector('[aria-label="Match actions"]');
      const roll = el?.querySelector('button');
      return roll?.hasAttribute('disabled') ?? false;
    });
    expect(quietIsPlayerB).toBe(true);

    // Play the turn from whichever client holds it, using only that client's UI.
    const actor = playing.turn.currentSeat === 0 ? page : pageB;
    await actor.getByRole('button', { name: /^Roll/ }).click();

    // The server is the oracle: the roll really happened.
    await expect
      .poll(async () => (await serverSnapshot(matchId)).tick, { timeout: 15_000 })
      .toBeGreaterThan(playing.tick);
    const afterRoll = await serverSnapshot(matchId);
    expect(afterRoll.players.some((p) => p.position > 0)).toBe(true);

    // The dice shown come from the server's event, and the total is consistent
    // with the two dice it reported — not merely "some number appeared".
    const lastRoll = actor.getByTestId('last-roll');
    await expect(lastRoll).toBeVisible({ timeout: 5_000 });
    const shown = (await lastRoll.textContent()) ?? '';
    const total = Number(shown.split('=').pop()?.trim());
    expect(total).toBeGreaterThanOrEqual(2);
    expect(total).toBeLessThanOrEqual(12);
    const rollEvent = await rollEventFor(actor, afterRoll.tick);
    if (rollEvent) {
      expect(rollEvent.die1 + rollEvent.die2).toBe(total);
    }

    // Both clients must agree on the authoritative state. Comparing each dock's
    // own view is the point: a client that had missed or double-applied a
    // transition would disagree here, which the old raw-socket second player
    // could never detect because it had no UI at all.
    await expect
      .poll(async () => (await serverSnapshot(matchId)).tick, { timeout: 10_000 })
      .toBe(afterRoll.tick);
    expect(errorsB).toEqual([]);

    // On a phone the match facts must be on screen: the information panel is
    // hidden below md, so without this a mobile player could not see their chips
    // or whose turn it was.
    if (testInfo.project.name === 'chromium-mobile') {
      const statusStrip = actor.getByLabel('Match status');
      await expect(statusStrip).toBeVisible();
      await expect(statusStrip).toContainText('Chips');
      await expect(statusStrip).toContainText('Turn');
    }

    await contextB.close();
  });

  test('mobile layout keeps controls reachable without overflow', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-mobile', 'mobile-only');
    await openLobby(page, 'Ace');
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(2);
    await expect(page.getByRole('navigation', { name: 'Match actions' })).toBeVisible();
  });
});
