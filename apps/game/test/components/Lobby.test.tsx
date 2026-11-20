// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@solidjs/testing-library';
import userEvent from '@testing-library/user-event';
import { Lobby } from '../../src/components/Lobby.tsx';
import { gameStore } from '../../src/state/game-store.ts';

beforeEach(() => {
  gameStore.reset();
});

function renderLobby(over: Partial<Parameters<typeof Lobby>[0]> = {}) {
  const props = {
    name: 'Ace',
    setName: vi.fn(),
    onEnter: vi.fn(),
    onJoin: vi.fn(),
    busy: false,
    ...over,
  };
  render(() => <Lobby {...props} />);
  return props;
}

describe('Lobby', () => {
  it('keeps the labels and buttons the browser suite pins', () => {
    renderLobby();
    expect(screen.getByRole('heading', { name: 'Open a private table' })).toBeInTheDocument();
    expect(screen.getByLabelText('Display name')).toBeInTheDocument();
    expect(screen.getByLabelText('Match id')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create a table' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Join' })).toBeDisabled();
  });

  it('shows the busy wording while creating a match', () => {
    renderLobby({ busy: true });
    expect(screen.getByRole('button', { name: 'Creating match…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Join' })).toBeDisabled();
  });

  it('reports the display name as it is typed', async () => {
    const props = renderLobby();
    await userEvent.clear(screen.getByLabelText('Display name'));
    await userEvent.type(screen.getByLabelText('Display name'), 'Bit');
    expect(props.setName).toHaveBeenCalledWith('Bit');
  });

  it('joins by the id the player typed', async () => {
    const props = renderLobby();
    await userEvent.type(screen.getByLabelText('Match id'), 'm_abc');
    await userEvent.click(screen.getByRole('button', { name: 'Join' }));
    expect(props.onJoin).toHaveBeenCalledWith('m_abc');
  });

  it('surfaces a connection error as an alert', () => {
    gameStore.setLastError('action could not be sent');
    renderLobby();
    expect(screen.getByRole('alert')).toHaveTextContent('action could not be sent');
  });
});
