// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import { TopBar } from '../../src/components/TopBar.tsx';
import { applySnapshot, gameStore, setLocalPlayer } from '../../src/state/game-store.ts';
import { snapshot } from '../fixtures.ts';

beforeEach(() => {
  gameStore.reset();
  setLocalPlayer('p1');
});

describe('TopBar', () => {
  it('states the release and the match on every viewport', () => {
    applySnapshot(snapshot(), null, 1, false);
    render(() => <TopBar />);
    const header = screen.getByRole('banner');
    expect(header).toHaveTextContent(`v${__APP_VERSION__}`);
    expect(header).toHaveTextContent('match m_test');
  });

  it('names the connection state in player words', () => {
    gameStore.setStatus('disconnected');
    render(() => <TopBar />);
    const header = screen.getByRole('banner');
    expect(header).toHaveTextContent('Offline');
    expect(header).not.toHaveTextContent('disconnected');
    expect(screen.getByRole('img', { name: 'Connection: Offline' })).toBeInTheDocument();
  });

  it('updates when the connection changes after mount', () => {
    gameStore.setStatus('connecting');
    render(() => <TopBar />);
    expect(screen.getByRole('banner')).toHaveTextContent('Connecting…');
    gameStore.setStatus('connected');
    expect(screen.getByRole('banner')).toHaveTextContent('Connected');
  });
});
