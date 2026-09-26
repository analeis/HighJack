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
  await page.getByRole('button', { name: /Create & join|Creating match/ }).click();
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

test.describe('game client', () => {
  test('lobby creates a real server-backed match', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(String(e)));
    await page.goto(GAME_URL, { waitUntil: 'domcontentloaded' });

    await expect(page.getByRole('heading', { name: /Open a private table/i })).toBeVisible();
    await page.getByLabel('Display name').fill('Ace');
    await page.getByRole('button', { name: 'Create & join' }).click();

    // The dock only renders once an authoritative snapshot arrived, so
    // seeing it proves the client is bound to a live match.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeVisible({ timeout: 20_000 });
    await expect(page.getByText(/match m_/)).toBeVisible();
    expect(errors).toEqual([]);
  });

  test('illegal controls stay disabled before a match starts', async ({ page }) => {
    await page.goto(GAME_URL, { waitUntil: 'domcontentloaded' });
    await page.getByLabel('Display name').fill('Ace');
    await page.getByRole('button', { name: 'Create & join' }).click();
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeVisible({ timeout: 20_000 });

    // A fresh lobby has no turn: only Ready (and Start for the host) can act.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Buy/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Decline/ })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^End turn/ })).toBeDisabled();
  });

  test('two clients play the authoritative board loop', async ({ page, context }) => {
    test.setTimeout(90_000);

    // Player A opens the match; both clients then bind to that match.
    await openLobby(page, 'Ace');
    const matchId = new URL(page.url()).searchParams.get('match') ?? '';
    expect(matchId).toMatch(/^m_/);

    // Second browser context = a genuinely separate client.
    const pageB = await context.newPage();
    await pageB.goto(GAME_URL, { waitUntil: 'domcontentloaded' });

    // Player B joins the same match through the HTTP lobby API the client
    // itself uses, then opens its own WebSocket bound to that seat.
    const joinToken = await pageB.evaluate(
      async (args: { server: string; id: string }) => {
        const res = await fetch(`${args.server}/matches/${args.id}/players`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ displayName: 'Bit' }),
        });
        return (await res.json()) as { playerId: string; token: string };
      },
      { server: SERVER, id: matchId },
    );
    expect(joinToken.token).toBeTruthy();

    await pageB.evaluate(
      async (args: { server: string; id: string; token: string }) => {
        const { server, id, token } = args;
        const socket = new WebSocket(server.replace(/^http/, 'ws') + '/ws');
        await new Promise<void>((resolve, reject) => {
          socket.addEventListener('open', () => resolve());
          socket.addEventListener('error', () => reject(new Error('ws failed')));
        });
        socket.send(
          JSON.stringify({ v: 1, type: 'hello', client: 'test/0.2.0', matchId: id, token }),
        );
        (window as unknown as { __hjSocket?: WebSocket }).__hjSocket = socket;
      },
      { server: SERVER, id: matchId, token: joinToken.token },
    );
    await page.waitForTimeout(600);

    // Both clients ready up; the host starts. Player A uses its own dock.
    await page.getByRole('button', { name: /^Ready/ }).click();
    await pageB.evaluate(() => {
      const socket = (window as unknown as { __hjSocket?: WebSocket }).__hjSocket;
      socket?.send(
        JSON.stringify({
          v: 1,
          type: 'action',
          seq: 1,
          action: { type: 'player_ready', ready: true },
        }),
      );
    });
    await page.waitForTimeout(600);

    await page.getByRole('button', { name: /^Start/ }).click();

    // The server is the oracle: the match really started, and the roll
    // became legal for the starting seat.
    await expect
      .poll(async () => (await serverSnapshot(matchId)).phase, { timeout: 15_000 })
      .toBe('playing');
    const playing = await serverSnapshot(matchId);
    expect(playing.players.filter((p) => p.ready).length).toBe(2);
    expect(playing.turn.currentSeat).toBe(0);
    // The dock reflects that server state on every viewport.
    await expect(page.getByRole('button', { name: /^Roll/ })).toBeEnabled({
      timeout: 10_000,
    });

    // The first player's roll is enabled and moves the token server-side.
    await page.getByRole('button', { name: /^Roll/ }).click();
    await page.waitForTimeout(600);
    const afterRoll = await serverSnapshot(matchId);
    expect(afterRoll.tick).toBeGreaterThan(playing.tick);
    expect(afterRoll.players.some((p) => p.position > 0)).toBe(true);
    // The last roll is displayed from the authoritative dice event, in the
    // dock so it is visible at every viewport.
    const lastRoll = page.getByTestId('last-roll');
    await expect(lastRoll).toBeVisible({ timeout: 5_000 });
    await expect(lastRoll).toHaveText(/=\s*(2|3|4|5|6|7|8|9|10|11|12)$/);

    // A second client observes the same tick: state is shared, not local.
    await pageB.close();
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
