// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import { BoardTextEquivalent } from '../../src/components/BoardView.tsx';
import { applySnapshot, gameStore, setLocalPlayer } from '../../src/state/game-store.ts';
import { snapshot } from '../fixtures.ts';

beforeEach(() => {
  gameStore.reset();
  setLocalPlayer('p1');
});

describe('the board in words', () => {
  it('keeps the structure the browser suite pins', () => {
    applySnapshot(snapshot({ phase: 'lobby' }), null, 1, false);
    const { container } = render(() => <BoardTextEquivalent />);
    const board = screen.getByLabelText('Board state');
    expect(board).toHaveTextContent('Lobby');
    expect(Array.from(board.querySelectorAll('h3')).map((h) => h.textContent)).toContain('Players');
    expect(container.querySelectorAll('table')).toHaveLength(2);
  });

  it('names player state and the end reason in player words', () => {
    applySnapshot(
      snapshot({
        phase: 'ended',
        endReason: 'last_standing',
        winnerId: 'p1',
        players: [
          {
            playerId: 'p1',
            name: 'Ace',
            seat: 0,
            money: 1500,
            status: 'eliminated',
            ready: false,
            isHost: false,
            position: 0,
          },
        ],
      }),
      null,
      1,
      false,
    );
    render(() => <BoardTextEquivalent />);
    const board = screen.getByLabelText('Board state');
    expect(board).toHaveTextContent('Out');
    expect(board).toHaveTextContent('last player standing');
    expect(board).not.toHaveTextContent('eliminated');
    expect(board).not.toHaveTextContent('last_standing');
  });
});
