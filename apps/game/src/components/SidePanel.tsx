import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Money } from '@highjack/ui';
import { currentPlayer, myHoldings } from '../state/game-store.ts';
import type { GameView } from '../state/game-store.ts';
import { playerStatusLabel } from './labels.ts';

/**
 * The side panel: the match at a glance.
 *
 * Hidden on narrow viewports, where the strip in the dock carries the same
 * facts. Both read the same view, so neither is a second source of truth.
 */
export function SidePanel(props: { state: GameView }): JSX.Element {
  const state = () => props.state;
  const me = () => state().players.find((p) => p.id === state().playerId) ?? null;
  const turnPlayer = () => currentPlayer(state());
  const holdings = () => myHoldings(state());
  return (
    <aside
      aria-label="Match information"
      class="absolute inset-y-0 left-0 z-10 hidden w-72 flex-col gap-6 overflow-y-auto border-r border-line-subtle bg-felt-950/95 p-6 md:flex"
    >
      <section>
        <h2 class="m-0 text-xs font-normal text-muted-500">Now playing</h2>
        <p class="mb-0 mt-1 font-display text-2xl text-cream-100">
          {turnPlayer()?.name ?? 'No one'}
        </p>
        <p class="mb-0 mt-1 text-sm text-muted-400">
          Round {state().turn.round} · {turnPhaseLabel(state().turn.phase)}
        </p>
        <Show when={state().turn.doublesStreak > 0}>
          <p class="mb-0 mt-2 text-sm text-gold-400">
            {state().turn.doublesStreak === 1
              ? 'A double'
              : `${state().turn.doublesStreak} doubles in a row`}
          </p>
        </Show>
      </section>

      <section>
        <h2 class="m-0 border-t border-line-subtle pt-5 text-xs font-normal text-muted-500">
          Your seat
        </h2>
        <Show when={me()} fallback={<p class="mb-0 mt-3 text-sm text-muted-500">Not seated</p>}>
          {(p) => (
            <dl class="m-0 mt-3 space-y-2 text-sm">
              <Row label="Seat" value={String(p().seat)} />
              <Row label="Chips" value={<Money amount={p().money} />} />
              <Row label="State" value={playerStatusLabel(p().status)} />
              <Row label="Holdings" value={String(holdings().length)} />
            </dl>
          )}
        </Show>
      </section>

      <section>
        <h2 class="m-0 border-t border-line-subtle pt-5 text-xs font-normal text-muted-500">
          Players
        </h2>
        <ul class="mb-0 mt-3 list-none space-y-2 p-0 text-sm">
          {state().players.map((p) => (
            <li
              class="flex items-center justify-between gap-2"
              classList={{ 'text-cream-100': p.seat === state().turn.currentSeat }}
            >
              <span class="truncate">
                {p.name}
                {p.isHost ? ' · host' : ''}
              </span>
              <span class="hj-numerals text-muted-400">{p.money.toLocaleString('en-US')}</span>
            </li>
          ))}
        </ul>
      </section>

      <Show when={state().lastRoll}>
        {(roll) => (
          <section>
            <h2 class="m-0 border-t border-line-subtle pt-5 text-xs font-normal text-muted-500">
              Last roll
            </h2>
            <p class="mb-0 mt-2 font-display text-3xl text-cream-100">
              {roll().die1} + {roll().die2} = {roll().die1 + roll().die2}
            </p>
          </section>
        )}
      </Show>
    </aside>
  );
}

function Row(props: { label: string; value: JSX.Element }): JSX.Element {
  return (
    <div class="flex items-center justify-between gap-2">
      <dt class="text-muted-500">{props.label}</dt>
      <dd class="m-0 text-cream-300">{props.value}</dd>
    </div>
  );
}

function turnPhaseLabel(phase: string): string {
  switch (phase) {
    case 'await_roll':
      return 'roll when ready';
    case 'await_buy_decision':
      return 'buy or pass';
    case 'turn_over':
      return 'turn finished';
    default:
      // An unknown future phase must not leave a dangling "Round N · ".
      return 'in play';
  }
}
