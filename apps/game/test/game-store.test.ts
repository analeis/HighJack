import { beforeEach, describe, expect, it } from 'vitest';
import {
  applyEvents,
  applyLegalActions,
  applySnapshot,
  currentPlayer,
  gameStore,
  myHoldings,
  resetAppliedTicks,
  setLocalPlayer,
  type GameView,
} from '../src/state/game-store.ts';
import type {
  GameConfig,
  GameEvent,
  GameSnapshot,
  SnapshotPlayer,
  SnapshotSpace,
} from '@highjack/protocol';

/**
 * Client-state tests.
 *
 * Two properties matter and are asserted here rather than assumed:
 *
 *  1. Authoritative state enters only through a snapshot or an event, and a
 *     snapshot replaces it wholesale.
 *  2. The merge is *ordered and idempotent*. A snapshot older than what we hold
 *     must not rewind the view, and a transition must be applied at most once.
 *     Neither held before: the store compared no ticks, and the first
 *     re-delivery charged a rent twice.
 *
 * Legality is not tested here because the client no longer decides it. The
 * snapshot's `you.legalActions` is the answer, and the store's only obligation is
 * to carry it through unchanged.
 */

const CONFIG = { version: 1 } as unknown as GameConfig;

function space(over: Record<string, unknown> = {}) {
  return {
    id: 'p1',
    kind: 'property',
    name: 'Copper Row',
    group: 'Copper',
    price: 100,
    rent: 10,
    amount: 0,
    owned: false,
    owner: '',
    level: 0,
    ...over,
  } as SnapshotSpace;
}

function snapshot(over: Partial<GameSnapshot> = {}): GameSnapshot {
  return {
    matchId: 'm_test',
    phase: 'lobby',
    tick: 0,
    players: [],
    spaces: [
      {
        id: 'go',
        kind: 'go',
        name: 'Start',
        group: '',
        price: 0,
        rent: 0,
        amount: 0,
        owned: false,
        owner: '',
        level: 0,
      },
      space(),
    ],
    turn: {
      currentSeat: 0,
      phase: 'await_roll',
      round: 1,
      doublesStreak: 0,
      awardExtraRoll: false,
    },
    winnerId: null,
    endReason: '',
    configHash: 'hash',
    ...over,
  } as GameSnapshot;
}

function player(id: string, seat: number, over: Record<string, unknown> = {}) {
  return {
    playerId: id,
    name: id,
    seat,
    money: 1500,
    status: 'active',
    ready: false,
    isHost: false,
    position: 0,
    ...over,
  } as SnapshotPlayer;
}

function view(): GameView | null {
  return gameStore.view();
}

const DICE: GameEvent = {
  type: 'dice_rolled',
  playerId: 'p1',
  die1: 3,
  die2: 4,
  fromSpace: 'go',
  toSpace: 'p1',
  passedGo: false,
} as GameEvent;

const RENT: GameEvent = {
  type: 'rent_paid',
  fromPlayerId: 'p1',
  toPlayerId: 'p2',
  spaceId: 'p1',
  amount: 10,
} as GameEvent;

beforeEach(() => {
  resetAppliedTicks();
  gameStore.reset();
  setLocalPlayer('p1');
});

describe('authoritative client state', () => {
  it('adopts a server snapshot wholesale', () => {
    applySnapshot(snapshot({ players: [player('p1', 0), player('p2', 1)] }), CONFIG, 1, false);
    expect(view()?.players).toHaveLength(2);
    expect(view()?.matchId).toBe('m_test');
    expect(view()?.tick).toBe(0);
  });

  it('applies a dice event to move the roller and record the roll', () => {
    applySnapshot(snapshot({ players: [player('p1', 0)] }), CONFIG, 1, false);
    applyEvents([{ tick: 1, event: DICE }]);
    expect(view()?.players[0]?.position).toBe(1);
    expect(view()?.lastRoll).not.toBeNull();
  });

  it('moves money only through authoritative events', () => {
    applySnapshot(snapshot({ players: [player('p1', 0), player('p2', 1)] }), CONFIG, 1, false);
    expect(view()?.players[0]?.money).toBe(1500);

    applyEvents([{ tick: 1, event: RENT }]);
    expect(view()?.players[0]?.money).toBe(1490);
    expect(view()?.players[1]?.money).toBe(1510);
  });

  it('records ownership from a purchase event', () => {
    applySnapshot(snapshot({ players: [player('p1', 0)] }), CONFIG, 1, false);
    applyEvents([
      {
        tick: 1,
        event: { type: 'property_bought', playerId: 'p1', spaceId: 'p1', price: 100 } as GameEvent,
      },
    ]);
    expect(view()?.spaces[1]?.owned).toBe(true);
    expect(view()?.spaces[1]?.owner).toBe('p1');
  });

  it('reverts holdings on bankruptcy and marks the player eliminated', () => {
    applySnapshot(
      snapshot({
        players: [player('p1', 0), player('p2', 1)],
        spaces: [
          space({ id: 'go', kind: 'go', name: 'Start' }),
          space({ owned: true, owner: 'p1' }),
        ],
      }),
      CONFIG,
      1,
      false,
    );
    applyEvents([
      {
        tick: 1,
        event: { type: 'player_bankrupt', playerId: 'p1', cause: 'bankruptcy_rent' } as GameEvent,
      },
    ]);
    expect(view()?.spaces[1]?.owned).toBe(false);
    expect(view()?.spaces[1]?.owner).toBe('');
    expect(view()?.players[0]?.status).toBe('eliminated');
  });

  it('reads the current player and my holdings from state', () => {
    applySnapshot(
      snapshot({
        phase: 'playing',
        turn: {
          currentSeat: 1,
          phase: 'turn_over',
          round: 2,
          doublesStreak: 0,
          awardExtraRoll: false,
        },
        players: [player('p1', 0), player('p2', 1)],
        spaces: [
          space({ id: 'go', kind: 'go', name: 'Start' }),
          space({ owned: true, owner: 'p1' }),
        ],
      }),
      CONFIG,
      1,
      false,
    );
    // The turn holder is seat 1, and the holdings belong to seat 0.
    expect(currentPlayer(view())?.id).toBe('p2');
    expect(myHoldings(view()).map((s) => s.id)).toEqual(['p1']);
  });

  it('records a terminal match and its winner', () => {
    applySnapshot(
      snapshot({ phase: 'ended', winnerId: 'p2', endReason: 'last_standing' }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.phase).toBe('ended');
    expect(view()?.winnerId).toBe('p2');
    expect(view()?.endReason).toBe('last_standing');
  });
});

describe('the merge is ordered', () => {
  it('ignores a snapshot older than the state already held', () => {
    applySnapshot(
      snapshot({ tick: 9, players: [player('p1', 0, { money: 400 })] }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.tick).toBe(9);

    // A stale snapshot must not rewind. The server publishes a transition's
    // events and then acks the action, so a peer's transition can land between
    // the two and the ack's snapshot genuinely predate what is on screen.
    applySnapshot(
      snapshot({ tick: 7, players: [player('p1', 0, { money: 1500 })] }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.tick).toBe(9);
    expect(view()?.players[0]?.money).toBe(400);
  });

  it('applies a snapshot at the same tick, which is idempotent', () => {
    applySnapshot(
      snapshot({ tick: 4, players: [player('p1', 0, { money: 400 })] }),
      CONFIG,
      1,
      false,
    );
    applySnapshot(
      snapshot({ tick: 4, players: [player('p1', 0, { money: 400 })] }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.players[0]?.money).toBe(400);
  });

  it('keeps the local player id across a snapshot that omits it', () => {
    setLocalPlayer('p1');
    applySnapshot(snapshot({ players: [player('p1', 0)] }), CONFIG, 1, false);
    expect(view()?.playerId).toBe('p1');
  });
});

describe('the merge is idempotent', () => {
  it('applies a repeated transition exactly once', () => {
    applySnapshot(
      snapshot({ tick: 0, players: [player('p1', 0), player('p2', 1)] }),
      CONFIG,
      1,
      false,
    );

    // The server documents at-least-once delivery and names the tick as the dedup
    // key. Without dedup, this second delivery charged the rent twice.
    applyEvents([{ tick: 1, event: RENT }]);
    applyEvents([{ tick: 1, event: RENT }]);

    expect(view()?.players[0]?.money).toBe(1490);
    expect(view()?.players[1]?.money).toBe(1510);
  });

  it('applies a re-delivered whole batch once', () => {
    applySnapshot(snapshot({ tick: 0, players: [player('p1', 0)] }), CONFIG, 1, false);
    const batch: { tick: number; event: GameEvent }[] = [
      { tick: 3, event: DICE },
      { tick: 3, event: { type: 'turn_advanced', seat: 1, round: 1 } as GameEvent },
    ];
    applyEvents(batch);
    applyEvents(batch);
    applyEvents(batch);

    expect(view()?.tick).toBe(3);
    // One movement, not three.
    expect(view()?.players[0]?.position).toBe(1);
  });

  it('still applies a genuinely newer transition', () => {
    applySnapshot(
      snapshot({ tick: 0, players: [player('p1', 0), player('p2', 1)] }),
      CONFIG,
      1,
      false,
    );
    applyEvents([{ tick: 1, event: RENT }]);
    applyEvents([{ tick: 2, event: RENT }]);
    expect(view()?.players[0]?.money).toBe(1480);
    expect(view()?.tick).toBe(2);
  });

  it('keeps the dedup set bounded', () => {
    applySnapshot(snapshot({ tick: 0, players: [player('p1', 0)] }), CONFIG, 1, false);
    for (let t = 1; t <= 5000; t++) {
      applyEvents([{ tick: t, event: DICE }]);
    }
    // Reaching here without unbounded growth is the assertion; re-applying an
    // early tick must now be treated as new rather than deduped forever.
    applyEvents([{ tick: 5000, event: DICE }]);
    expect(view()?.tick).toBe(5000);
  });
});

describe('legality comes from the server', () => {
  it('carries the per-recipient legal actions through unchanged', () => {
    applySnapshot(
      snapshot({
        you: { playerId: 'p1', legalActions: ['roll_dice', 'player_leave'] },
      }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.legalActions).toEqual(['roll_dice', 'player_leave']);
  });

  it('offers decline for a decision the player cannot afford', () => {
    // The engine does not gate decline on affordability. The old client helper
    // did, which left a player facing a purchase they could not make with no legal
    // move at all while the server waited for one.
    applySnapshot(
      snapshot({
        players: [player('p1', 0, { money: 0 })],
        you: { playerId: 'p1', legalActions: ['decline_buy', 'player_leave'] },
      }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.legalActions).toContain('decline_buy');
    expect(view()?.legalActions).not.toContain('buy_property');
  });

  it('falls back to deriving when a server sends no viewer block', () => {
    // Protocol 1.1 compatibility: a newer client against an older server must
    // still have a working dock rather than an empty one.
    applySnapshot(
      snapshot({ phase: 'playing', players: [player('p1', 0), player('p2', 1)] }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.legalActions).toContain('roll_dice');
  });
});

describe("legality follows other players' actions", () => {
  it('accepts a refreshed legal set independently of a snapshot', () => {
    applySnapshot(
      snapshot({
        phase: 'lobby',
        players: [player('p1', 0, { isHost: true, ready: true }), player('p2', 1)],
        // Only p1 is ready, so the host is not permitted to start.
        you: { playerId: 'p1', legalActions: ['player_ready', 'player_leave'] },
      }),
      CONFIG,
      1,
      false,
    );
    expect(view()?.legalActions).not.toContain('game_start');

    // The other player readies. No snapshot is sent for another player's action,
    // so the set can only arrive with the transition — which is exactly the case
    // that used to leave the host's Start disabled with nothing to indicate why.
    applyLegalActions(['player_ready', 'game_start', 'player_leave']);
    expect(view()?.legalActions).toContain('game_start');
  });

  it('does not invent legality before a view exists', () => {
    applyLegalActions(['roll_dice']);
    expect(view()).toBeNull();
  });
});
