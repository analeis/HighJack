// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import { SidePanel } from '../../src/components/SidePanel.tsx';
import { gameStore } from '../../src/state/game-store.ts';
import { player, view } from '../fixtures.ts';

beforeEach(() => {
  gameStore.reset();
});

describe('SidePanel', () => {
  it('keeps the four section headings', () => {
    render(() => <SidePanel state={view({ lastRoll: { die1: 3, die2: 4, byId: 'p1' } })} />);
    const panel = screen.getByLabelText('Match information');
    // A heading is a navigation anchor for screen readers; a styled <p>
    // looks identical but cannot be reached with the heading keys.
    expect(Array.from(panel.querySelectorAll('h2')).map((h) => h.textContent)).toEqual([
      'Now playing',
      'Your seat',
      'Players',
      'Last roll',
    ]);
  });

  it('names player states in player words', () => {
    render(() => <SidePanel state={view({ players: [player({ status: 'eliminated' })] })} />);
    const panel = screen.getByLabelText('Match information');
    expect(panel).toHaveTextContent('Out');
    expect(panel).not.toHaveTextContent('eliminated');
  });

  it('shows the round without engine vocabulary', () => {
    render(() => <SidePanel state={view({ turn: { ...view().turn, round: 7 } })} />);
    const panel = screen.getByLabelText('Match information');
    expect(panel).toHaveTextContent('Round 7');
    expect(panel).not.toHaveTextContent('tick');
  });

  it('phrases a doubles streak as a player event', () => {
    const base = view();
    render(() => (
      <SidePanel
        state={view({
          turn: { ...base.turn, doublesStreak: 2 },
        })}
      />
    ));
    expect(screen.getByLabelText('Match information')).toHaveTextContent('2 doubles in a row');
  });

  it('does not leave a dangling separator for an unknown phase', () => {
    const base = view();
    render(() => (
      <SidePanel
        state={view({ turn: { ...base.turn, phase: 'future_phase' as typeof base.turn.phase } })}
      />
    ));
    const text = screen.getByLabelText('Match information').textContent ?? '';
    expect(text).toContain('Round 3 · in play');
    expect(text.trimEnd().endsWith('·')).toBe(false);
  });
});
