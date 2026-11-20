import type { GameConfig, GameSnapshot } from '@highjack/protocol';
import type { GameView, UiPlayer, UiSpace } from '../src/state/game-store.ts';

/** Test data for the player-facing components. */
export function player(over: Partial<UiPlayer> = {}): UiPlayer {
  return {
    id: 'p1',
    name: 'Ace',
    seat: 0,
    money: 1500,
    status: 'active',
    ready: false,
    isHost: false,
    position: 0,
    ...over,
  };
}

export function space(over: Partial<UiSpace> = {}): UiSpace {
  return {
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
    ...over,
  };
}

/** A wire snapshot for tests that drive the module-level store. */
export function snapshot(over: Partial<GameSnapshot> = {}): GameSnapshot {
  return {
    matchId: 'm_test',
    phase: 'playing',
    tick: 1,
    players: [
      {
        playerId: 'p1',
        name: 'Ace',
        seat: 0,
        money: 1500,
        status: 'active',
        ready: false,
        isHost: false,
        position: 0,
      },
    ],
    spaces: [],
    turn: {
      currentSeat: 0,
      phase: 'await_roll',
      round: 3,
      doublesStreak: 0,
      awardExtraRoll: false,
    },
    winnerId: null,
    endReason: '',
    configHash: 'hash',
    ...over,
  } as GameSnapshot;
}

const CONFIG = { version: 1 } as unknown as GameConfig;

export function view(over: Partial<GameView> = {}): GameView {
  return {
    matchId: 'm_test',
    phase: 'playing',
    tick: 1,
    legalActions: [],
    players: [player()],
    spaces: [
      space(),
      space({ id: 'copper', kind: 'property', name: 'Copper Row', price: 100, rent: 10 }),
    ],
    turn: {
      currentSeat: 0,
      phase: 'await_roll',
      round: 3,
      doublesStreak: 0,
      awardExtraRoll: false,
    },
    winnerId: null,
    endReason: '',
    playerId: 'p1',
    lastRoll: null,
    config: CONFIG,
    ...over,
  };
}
