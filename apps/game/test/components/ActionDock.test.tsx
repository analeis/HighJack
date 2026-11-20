// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import userEvent from '@testing-library/user-event';
import { ActionDock } from '../../src/components/ActionDock.tsx';
import { gameStore } from '../../src/state/game-store.ts';
import { view } from '../fixtures.ts';

/**
 * The dock is the only control surface a player has. These tests pin the two
 * things the redesign could silently break without any gate noticing: the
 * one-primary-at-a-time hierarchy, and the words the buttons expose.
 */
beforeEach(() => {
  gameStore.reset();
});

function renderDock(over: Parameters<typeof view>[0] = {}, onAct = vi.fn()) {
  render(() => <ActionDock state={view(over)} onAct={onAct} />);
  return onAct;
}

function button(label: string) {
  return screen.getByRole('button', { name: new RegExp(`^${label}:`) });
}

describe('ActionDock hierarchy', () => {
  it('fills exactly the action the engine allows', () => {
    renderDock({ legalActions: ['roll_dice'] });
    expect(button('Roll')).toBeEnabled();
    expect(button('Roll').className).toContain('bg-gold-500');
    for (const label of ['Start', 'Buy', 'Decline', 'End turn']) {
      expect(button(label)).toBeDisabled();
      expect(button(label).className).not.toContain('bg-gold-500');
    }
  });

  it('promotes whichever turn action is legal, never two at once', () => {
    for (const [actions, primary] of [
      [['roll_dice'], 'Roll'],
      [['buy_property'], 'Buy'],
      [['end_turn'], 'End turn'],
      [['game_start'], 'Start'],
    ] as const) {
      gameStore.reset();
      const { unmount } = render(() => (
        <ActionDock state={view({ legalActions: [...actions] })} onAct={vi.fn()} />
      ));
      const filled = screen
        .getAllByRole('button')
        .filter((b) => b.className.includes('bg-gold-500'));
      expect(filled.map((b) => b.textContent)).toEqual([primary]);
      unmount();
    }
  });

  it('keeps a pass action secondary even when it is the only legal move', () => {
    renderDock({ legalActions: ['decline_buy'] });
    expect(button('Decline')).toBeEnabled();
    expect(button('Decline').className).not.toContain('bg-gold-500');
    expect(
      screen.getAllByRole('button').filter((b) => b.className.includes('bg-gold-500')),
    ).toEqual([]);
  });

  it('keeps every action name in the form the browser suite pins', () => {
    renderDock({
      legalActions: [
        'player_ready',
        'game_start',
        'roll_dice',
        'buy_property',
        'decline_buy',
        'end_turn',
      ],
    });
    for (const label of ['Ready', 'Start', 'Roll', 'Buy', 'Decline', 'End turn']) {
      expect(button(label)).toBeInTheDocument();
      expect(button(label).getAttribute('title')).toBeTruthy();
    }
  });

  it('releases enabled actions when one is in flight', () => {
    renderDock({ legalActions: ['roll_dice'] });
    gameStore.setPendingAction(true);
    expect(button('Roll')).toBeDisabled();
  });
});

describe('ActionDock words and callbacks', () => {
  it('sends the ready toggle with the opposite of the current state', async () => {
    const onAct = renderDock({ legalActions: ['player_ready'] });
    await userEvent.click(button('Ready'));
    expect(onAct).toHaveBeenCalledWith('player_ready', { ready: true });
  });

  it('shows the last roll when the server reports one', () => {
    renderDock({ lastRoll: { die1: 3, die2: 4, byId: 'p1' } });
    expect(screen.getByTestId('last-roll')).toHaveTextContent('3 + 4 = 7');
  });

  it('names the end reason in player words', () => {
    renderDock({ phase: 'ended', endReason: 'last_standing', winnerId: 'p1' });
    expect(screen.getByRole('status')).toHaveTextContent('Ace wins (last player standing)');
    expect(screen.getByRole('status')).not.toHaveTextContent('last_standing');
  });
});
