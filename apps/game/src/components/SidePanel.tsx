import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Badge, Money } from '@highjack/ui';
import { currentPlayer, myHoldings } from '../state/game-store.ts';
import type { GameView } from '../state/game-store.ts';

/**
 * The side panel: the full match information.
 *
 * It is hidden on narrow viewports, where the status strip in the action dock
 * carries the same facts. Both read the same view, so neither is a second
 * source of truth.
 */
export function SidePanel(props: { state: GameView }): JSX.Element {
  const state = () => props.state;
  const me = () => state().players.find((p) => p.id === state().playerId) ?? null;
  const turnPlayer = () => currentPlayer(state());
  const holdings = () => myHoldings(state());
  return (
    <aside
      aria-label="Match information"
      class="absolute inset-y-0 left-0 z-10 hidden w-72 flex-col gap-4 overflow-y-auto border-r border-line-subtle bg-felt-900/95 p-4 md:flex"
    >
      <section>
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">Turn</h2>
        <p class="mt-2 mb-0 text-sm text-cream-300">
          {turnPlayer()?.name ?? '—'}
          <Show
            when={turnPlayer()}
            fallback={<span class="text-muted-500"> (no active player)</span>}
          >
            <span class="text-muted-500"> · {turnPhaseLabel(state().turn.phase)}</span>
          </Show>
        </p>
        <p class="mt-1 mb-0 font-mono text-xs text-muted-500">
          round {state().turn.round} · tick {state().tick}
        </p>
        <Show when={state().turn.doublesStreak > 0}>
          <p class="mt-1 mb-0 font-mono text-xs text-gold-400">
            doubles ×{state().turn.doublesStreak}
            {state().turn.awardExtraRoll ? ' · extra roll earned' : ''}
          </p>
        </Show>
      </section>

      <section>
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">You</h2>
        <Show
          when={me()}
          fallback={<p class="mt-2 mb-0 text-sm text-muted-500">not bound to a seat</p>}
        >
          {(p) => (
            <dl class="m-0 mt-2 space-y-1.5 text-sm">
              <Row label="Seat" value={String(p().seat)} />
              <Row label="Chips" value={<Money amount={p().money} />} />
              <Row
                label="Status"
                value={
                  <Badge tone={p().status === 'active' ? 'teal' : 'crimson'}>{p().status}</Badge>
                }
              />
              <Row label="Holdings" value={String(holdings().length)} />
            </dl>
          )}
        </Show>
      </section>

      <section>
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">Table</h2>
        <ul class="mb-0 mt-2 list-none space-y-1.5 p-0 text-sm">
          {state().players.map((p) => (
            <li
              class="flex items-center justify-between gap-2"
              classList={{
                'text-cream-100': p.seat === state().turn.currentSeat,
              }}
            >
              <span class="truncate">
                {p.name}
                {p.isHost ? ' ★' : ''}
              </span>
              <span class="hj-numerals text-muted-400">{p.money.toLocaleString('en-US')}</span>
            </li>
          ))}
        </ul>
      </section>

      <Show when={state().lastRoll}>
        {(roll) => (
          <section>
            <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">Last roll</h2>
            <p class="mt-2 mb-0 font-display text-2xl font-extrabold text-cream-100">
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
      return 'awaiting roll';
    case 'await_buy_decision':
      return 'property decision';
    case 'turn_over':
      return 'ready to end turn';
    default:
      return phase;
  }
}
