// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import { Announcer } from '../../src/components/Announcer.tsx';
import { StatusBanner } from '../../src/components/StatusBanner.tsx';
import { applySnapshot, gameStore, setLocalPlayer } from '../../src/state/game-store.ts';
import { snapshot } from '../fixtures.ts';

beforeEach(() => {
  gameStore.reset();
  setLocalPlayer('p1');
});

describe('StatusBanner', () => {
  it('says nothing while connected', () => {
    gameStore.setStatus('connected');
    render(() => <StatusBanner />);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('names a dropped connection in player words', () => {
    gameStore.setStatus('disconnected');
    render(() => <StatusBanner />);
    expect(screen.getByRole('status')).toHaveTextContent('Offline');
    expect(screen.getByRole('status')).not.toHaveTextContent('disconnected');
  });

  it('reports a resync without engine vocabulary', () => {
    applySnapshot(snapshot(), null, 2, true);
    gameStore.setStatus('connected');
    render(() => <StatusBanner />);
    const banner = screen.getByText('Caught up with the server');
    expect(banner).toBeInTheDocument();
    expect(banner).not.toHaveTextContent('state resynced');
  });
});

describe('Announcer', () => {
  it('mounts the live region before it has anything to say', () => {
    gameStore.setStatus('connected');
    const { container } = render(() => <Announcer />);
    const live = container.querySelector('[role="status"]')!;
    expect(live).toHaveAttribute('aria-live', 'polite');
    expect(live).toHaveAttribute('aria-atomic', 'true');
    expect(live.textContent).toBe('');
  });

  it('announces a connection change in player words', () => {
    gameStore.setStatus('error');
    render(() => <Announcer />);
    expect(screen.getByRole('status')).toHaveTextContent('Connection lost.');
    expect(screen.getByRole('status')).not.toHaveTextContent('error');
  });

  it('announces whose turn it is', () => {
    gameStore.setStatus('connected');
    applySnapshot(snapshot(), null, 1, false);
    render(() => <Announcer />);
    expect(screen.getByRole('status')).toHaveTextContent('Your turn.');
  });

  it('announces a lobby count while waiting for players', () => {
    gameStore.setStatus('connected');
    applySnapshot(snapshot({ phase: 'lobby' }), null, 1, false);
    render(() => <Announcer />);
    expect(screen.getByRole('status')).toHaveTextContent('Lobby: 0 of 1 ready.');
  });
});
