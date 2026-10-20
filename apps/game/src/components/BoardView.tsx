import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { gameStore } from '../state/game-store.ts';
import type { GameView } from '../state/game-store.ts';

/**
 * The board surface: the canvas, and the text equivalent of it.
 *
 * The canvas is opaque to assistive technology, so the text equivalent is not a
 * fallback — it is the board for anyone who cannot see the canvas. Both read
 * the same authoritative state, and neither derives anything of its own.
 */
export function StageCanvas(props: { ref: (el: HTMLCanvasElement) => void }): JSX.Element {
  return (
    <div class="absolute inset-0 md:left-72">
      {/*
        The canvas is decorative and hidden from assistive technology. Its previous
        label described the rendering method — "rendered from authoritative match
        state" — which told a screen-reader user nothing about the board: no dice,
        no properties, no prices, no positions, no turn. BoardTextEquivalent is the
        accessible representation, and it carries the same state.
      */}
      <canvas ref={props.ref} class="h-full w-full" aria-hidden="true" />
    </div>
  );
}

/**
 * The board, in words.
 *
 * A canvas is opaque to assistive technology, so the board needs a text equivalent
 * carrying the same authoritative state. It is visually hidden but present in the
 * accessibility tree, and it is a real structure — headings and tables — rather than
 * one long string, so a screen-reader user can navigate to the part they want
 * instead of hearing the whole board on every change.
 */

export function BoardTextEquivalent(): JSX.Element {
  const view = gameStore.view();
  return (
    <div class="sr-only" aria-label="Board state">
      <Show when={view} fallback={<p>No active match.</p>}>
        {(v) => {
          const board = v();
          const active = board.players.find(
            (p) => p.seat === board.turn.currentSeat && p.status === 'active',
          );
          const roll = board.lastRoll;
          return (
            <>
              <h2>Match {board.matchId}</h2>
              <p>
                {describePhase(board)} Round {board.turn.round}.{' '}
                {active
                  ? `${active.name} to act${board.playerId === active.id ? ' — that is you' : ''}.`
                  : 'No player has the turn.'}{' '}
                {roll
                  ? `Last roll ${roll.die1} and ${roll.die2} by ${
                      board.players.find((p) => p.id === roll.byId)?.name ?? 'unknown'
                    }.`
                  : 'No dice rolled yet.'}
                {board.winnerId
                  ? ` Winner: ${board.players.find((p) => p.id === board.winnerId)?.name ?? board.winnerId}.`
                  : ''}
              </p>

              <h3>Players</h3>
              <table>
                <caption>Players, chips and positions</caption>
                <thead>
                  <tr>
                    <th scope="col">Seat</th>
                    <th scope="col">Player</th>
                    <th scope="col">Chips</th>
                    <th scope="col">On</th>
                    <th scope="col">State</th>
                  </tr>
                </thead>
                <tbody>
                  {board.players.map((p) => (
                    <tr>
                      <td>{p.seat + 1}</td>
                      <td>
                        {p.name}
                        {p.id === board.playerId ? ' (you)' : ''}
                      </td>
                      <td>{p.money.toLocaleString('en-US')}</td>
                      <td>{board.spaces[p.position]?.name ?? 'off board'}</td>
                      <td>
                        {[
                          p.status !== 'active' ? p.status : null,
                          p.isHost ? 'host' : null,
                          p.ready ? 'ready' : 'not ready',
                        ]
                          .filter(Boolean)
                          .join(', ')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <h3>Spaces</h3>
              <table>
                <caption>Spaces in board order, with price, rent and owner</caption>
                <thead>
                  <tr>
                    <th scope="col">Space</th>
                    <th scope="col">Price</th>
                    <th scope="col">Rent</th>
                    <th scope="col">Owner</th>
                  </tr>
                </thead>
                <tbody>
                  {board.spaces.map((space) => (
                    <tr>
                      <td>
                        {space.name}
                        {space.level > 0 ? ` (level ${space.level})` : ''}
                      </td>
                      <td>
                        {space.kind === 'property' ? space.price.toLocaleString('en-US') : '—'}
                      </td>
                      <td>{space.rent.toLocaleString('en-US')}</td>
                      <td>
                        {space.owned
                          ? (board.players.find((p) => p.id === space.owner)?.name ?? 'unknown')
                          : 'unowned'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          );
        }}
      </Show>
    </div>
  );
}

function describePhase(v: GameView): string {
  switch (v.phase) {
    case 'lobby':
      return 'Lobby, waiting for players.';
    case 'playing':
      return v.turn.phase === 'await_buy_decision'
        ? 'In play, deciding whether to buy.'
        : v.turn.phase === 'await_roll'
          ? 'In play, rolling.'
          : 'In play.';
    case 'ended':
      return `Finished${v.endReason ? `: ${v.endReason}` : ''}.`;
    case 'interrupted':
      return 'Interrupted.';
    default:
      return '';
  }
}

/**
 * A live region that is mounted once and never removed.
 *
 * The dock and the side panel each carried `role="status"` on elements that were
 * mounted and unmounted with the state they reported. A live region has to be in the
 * accessibility tree *before* its content changes, so a region that arrives with the
 * message it is announcing usually says nothing at all. This one is always present;
 * only its text changes.
 */
