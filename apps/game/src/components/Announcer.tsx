import type { JSX } from 'solid-js';
import { gameStore } from '../state/game-store.ts';

/**
 * The live region that announces turn, dice and connection changes.
 *
 * It is mounted once and never removed. A live region has to be in the
 * accessibility tree *before* its content changes, so a region that arrives with
 * the message it is announcing usually says nothing at all.
 */
export function Announcer(): JSX.Element {
  // The accessors themselves, not their values: `message` is a reactive function,
  // and `gameStore.status()` would capture the value once and never update, leaving
  // a live region whose text is frozen at the connection state it saw on mount.
  const view = gameStore.view;
  const status = gameStore.status;
  const detail = gameStore.statusDetail;
  const message = () => {
    const v = view();
    if (status() !== 'connected') {
      return detail() || `Connection ${status()}.`;
    }
    if (!v) return '';
    const active = v.players.find((p) => p.seat === v.turn.currentSeat && p.status === 'active');
    if (v.phase === 'lobby') {
      const waiting = v.players.filter((p) => p.ready).length;
      return `Lobby: ${waiting} of ${v.players.length} ready.`;
    }
    if (!active) return '';
    const turn = v.playerId === active.id ? 'Your turn.' : `${active.name}'s turn.`;
    const roll = v.lastRoll;
    return `${turn}${
      roll
        ? ` Rolled ${roll.die1} and ${roll.die2}${
            v.turn.phase === 'await_buy_decision' ? '. Choose whether to buy.' : '.'
          }`
        : ''
    }`;
  };
  return (
    <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
      {message()}
    </div>
  );
}
