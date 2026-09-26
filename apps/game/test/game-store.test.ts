import { describe, expect, it } from 'vitest';
import {
  applyEvents,
  applySnapshot,
  gameStore,
  setLocalPlayer,
  canBuy,
  canEndTurn,
  canRoll,
  currentPlayer,
  isMyTurn,
  myHoldings,
  type GameView,
} from '../src/state/game-store.ts';
import type { GameConfig, GameEvent, GameSnapshot } from '@highjack/protocol';

/**
 * Client-state tests prove the reducer is the only path into authoritative
 * state: snapshots replace it, events mutate it through pure rules, and
 * the legality helpers read it without ever deciding outcomes.
 */

const CONFIG = { version: 1 } as unknown as GameConfig;

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
      {
        id: 'a1',
        kind: 'property',
        name: 'Copper Row',
        group: 'Copper',
        price: 100,
        rent: 10,
        amount: 0,
        owned: false,
        owner: '',
        level: 0,
      },
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
  };
}

describe('authoritative client state', () => {
  it('adopts a server snapshot wholesale', () => {
    applySnapshot(
      snapshot({ phase: 'playing', tick: 7, players: [player('p1', 0), player('p2', 1)] as never }),
      CONFIG,
      1,
      false,
    );
    const view = gameView();
    expect(view.phase).toBe('playing');
    expect(view.tick).toBe(7);
    expect(view.players).toHaveLength(2);
  });

  it('applies a dice event to move the roller and record the roll', () => {
    applySnapshot(
      snapshot({ phase: 'playing', players: [player('p1', 0), player('p2', 1)] as never }),
      CONFIG,
      1,
      false,
    );
    applyEvents([{ tick: 1, event: diceRolled('p1', 3, 4) }]);
    const view = gameView();
    const p1 = view.players.find((p) => p.id === 'p1');
    expect(p1?.position).toBe(1); // 0 + 7 wraps onto a1 (index 1)
    expect(view.lastRoll).toEqual({ die1: 3, die2: 4, byId: 'p1' });
  });

  it('moves money only through authoritative events', () => {
    applySnapshot(
      snapshot({ phase: 'playing', players: [player('p1', 0), player('p2', 1)] as never }),
      CONFIG,
      1,
      false,
    );
    applyEvents([
      {
        tick: 1,
        event: {
          type: 'rent_paid',
          fromPlayerId: 'p1',
          toPlayerId: 'p2',
          spaceId: 'a1',
          amount: 10,
        } as GameEvent,
      },
    ]);
    const view = gameView();
    expect(view.players.find((p) => p.id === 'p1')?.money).toBe(1490);
    expect(view.players.find((p) => p.id === 'p2')?.money).toBe(1510);
  });

  it('records ownership from a purchase event', () => {
    applySnapshot(
      snapshot({ phase: 'playing', players: [player('p1', 0, { position: 1 })] as never }),
      CONFIG,
      1,
      false,
    );
    applyEvents([
      {
        tick: 1,
        event: { type: 'property_bought', playerId: 'p1', spaceId: 'a1', price: 100 } as GameEvent,
      },
    ]);
    const view = gameView();
    const a1 = view.spaces.find((s) => s.id === 'a1');
    expect(a1?.owned).toBe(true);
    expect(a1?.owner).toBe('p1');
    expect(view.players[0]?.money).toBe(1400);
  });

  it('reverts holdings on bankruptcy and marks the player eliminated', () => {
    applySnapshot(
      snapshot({
        phase: 'playing',
        players: [player('p1', 0), player('p2', 1)] as never,
        spaces: [
          snapshot().spaces[0]!,
          { ...snapshot().spaces[1]!, owned: true, owner: 'p2' },
        ] as never,
      }),
      CONFIG,
      1,
      false,
    );
    applyEvents([
      {
        tick: 1,
        event: {
          type: 'player_bankrupt',
          playerId: 'p2',
          cause: 'bankruptcy_rent',
          amountOwed: 40,
        } as GameEvent,
      },
    ]);
    const view = gameView();
    expect(view.spaces.find((s) => s.id === 'a1')?.owned).toBe(false);
    expect(view.players.find((p) => p.id === 'p2')?.status).toBe('eliminated');
  });

  it('gates legal actions on the authoritative turn, not on local intent', () => {
    applySnapshot(
      snapshot({
        phase: 'playing',
        turn: {
          currentSeat: 0,
          phase: 'await_roll',
          round: 1,
          doublesStreak: 0,
          awardExtraRoll: false,
        },
        players: [player('p1', 0), player('p2', 1, { position: 1, money: 50 })] as never,
      }),
      CONFIG,
      1,
      false,
    );
    setLocalPlayer('p2');
    const view = gameView();
    // p2 is not the current player: nothing is actionable for them.
    expect(isMyTurn(view)).toBe(false);
    expect(canRoll(view)).toBe(false);
    expect(canBuy(view)).toBe(false);
    expect(canEndTurn(view)).toBe(false);
  });

  it('enables buy only when an affordable unowned property decision is pending', () => {
    applySnapshot(
      snapshot({
        phase: 'playing',
        turn: {
          currentSeat: 0,
          phase: 'await_buy_decision',
          round: 1,
          doublesStreak: 0,
          awardExtraRoll: false,
        },
        players: [player('p1', 0, { position: 1, money: 500 })] as never,
      }),
      CONFIG,
      1,
      false,
    );
    setLocalPlayer('p1');
    expect(canBuy(gameView())).toBe(true);
  });

  it('never lets a client act for an eliminated player', () => {
    applySnapshot(
      snapshot({
        phase: 'playing',
        players: [player('p1', 0, { status: 'eliminated' })] as never,
      }),
      CONFIG,
      1,
      false,
    );
    setLocalPlayer('p1');
    const view = gameView();
    expect(isMyTurn(view)).toBe(false);
    expect(canRoll(view)).toBe(false);
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
        players: [player('p1', 0), player('p2', 1)] as never,
        spaces: [
          snapshot().spaces[0]!,
          { ...snapshot().spaces[1]!, owned: true, owner: 'p1' },
        ] as never,
      }),
      CONFIG,
      1,
      false,
    );
    setLocalPlayer('p1');
    const view = gameView();
    expect(currentPlayer(view)?.id).toBe('p2');
    expect(myHoldings(view).map((s) => s.id)).toEqual(['a1']);
    expect(canEndTurn(view)).toBe(false); // p1 is not the current player
  });

  it('records a terminal match and its winner', () => {
    applySnapshot(
      snapshot({ phase: 'playing', players: [player('p1', 0), player('p2', 1)] as never }),
      CONFIG,
      1,
      false,
    );
    applyEvents([
      {
        tick: 9,
        event: { type: 'game_ended', winnerId: 'p1', reason: 'last_standing' } as GameEvent,
      },
    ]);
    const view = gameView();
    expect(view.phase).toBe('ended');
    expect(view.winnerId).toBe('p1');
    expect(view.endReason).toBe('last_standing');
  });
});

function diceRolled(by: string, die1: number, die2: number): GameEvent {
  return {
    type: 'dice_rolled',
    playerId: by,
    die1,
    die2,
    fromSpace: 'go',
    toSpace: 'a1',
    passedGo: false,
  } as GameEvent;
}

function gameView(): GameView {
  const view = gameStore.view();
  if (!view) throw new Error('view not set');
  return view;
}
