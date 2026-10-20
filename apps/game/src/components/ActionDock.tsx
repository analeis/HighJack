import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import type { ActionType } from '@highjack/protocol';
import { currentPlayer, gameStore, myHoldings } from '../state/game-store.ts';
import type { GameView } from '../state/game-store.ts';

/**
 * The action dock: the only controls a player has.
 *
 * What the dock offers is decided by the engine, not here: the server sends the
 * legal actions for this player with every snapshot and every transition, and
 * the dock renders them. It does not re-derive affordability, turn ownership or
 * start eligibility.
 */
export function ActionDock(props: {
  state: GameView;
  onAct: (type: ActionType, extra?: Record<string, unknown>) => Promise<void>;
}): JSX.Element {
  const state = () => props.state;
  const me = () => state().players.find((p) => p.id === state().playerId) ?? null;
  const space = () => (me() ? state().spaces[me()!.position] : undefined);
  const pending = () => gameStore.pendingAction();
  const turnPlayer = () => currentPlayer(state());
  const mySpaces = () => myHoldings(state());
  /**
   * Whether the engine would accept this action from this player right now.
   *
   * Read from the snapshot's per-recipient `you.legalActions` rather than derived
   * locally: the client used to re-implement affordability, turn ownership and
   * start eligibility, and those copies drifted from the engine's rules.
   */
  const allowed = (action: ActionType): boolean => (state()?.legalActions ?? []).includes(action);
  const ended = () => state().phase === 'ended';

  return (
    <>
      {/* The full match-information panel is `hidden` below `md`, which left a
          phone player with no chip balance, no indication of whose turn it was,
          no round counter and no player list — while being asked to decide whether
          to buy a property whose only price hint was a `title` attribute iOS does
          not show on tap. This strip carries the facts a player cannot play
          without, and is the mobile counterpart of the panel rather than a
          second source of truth: everything in it is read from the same view. */}
      <section
        aria-label="Match status"
        class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line-subtle bg-felt-900/90 px-4 py-2 text-xs md:hidden"
      >
        <span class="text-muted-400">
          Chips <strong class="font-mono text-cream-100">{me()?.money ?? 0}</strong>
        </span>
        <span class="text-muted-400">
          Turn{' '}
          <strong class="text-cream-100">
            {turnPlayer()?.name ?? '—'}
            {turnPlayer() && turnPlayer()?.id === me()?.id ? ' (you)' : ''}
          </strong>
        </span>
        <span class="text-muted-400">
          Round <strong class="font-mono text-cream-100">{state().turn.round}</strong>
        </span>
        <span class="text-muted-400">
          {state().phase === 'lobby'
            ? me()?.ready
              ? 'Ready'
              : 'Not ready'
            : `${state().players.filter((p) => p.status === 'active').length} in`}
        </span>
        <Show when={mySpaces().length > 0}>
          <span class="text-muted-400">
            Holdings <strong class="font-mono text-cream-100">{mySpaces().length}</strong>
          </span>
        </Show>
      </section>
      {/* `role="group"`, not `<nav>`: nothing here navigates. A navigation
          landmark containing only buttons misleads a screen-reader user into
          expecting links to move between pages. */}
      <div
        role="group"
        aria-label="Match actions"
        class="z-10 flex flex-wrap items-center justify-center gap-2 border-t border-line-subtle bg-felt-900/90 px-4 py-3"
      >
        <Show when={ended()}>
          <p role="status" class="mr-2 text-sm text-gold-400">
            {state().winnerId
              ? `${state().players.find((p) => p.id === state().winnerId)?.name ?? 'Winner'} wins (${state().endReason})`
              : `match ended — ${state().endReason}`}
          </p>
        </Show>

        {/* A real toggle, not a one-way latch. The engine accepts the toggle in
          both directions, and it used to be one-way in the UI: a player who
          inherited the host role after the host left could not withdraw, and every
          board control was disabled for them, so the lobby was stuck until a
          reload. */}
        <ActionButton
          label={me()?.ready ? 'Not ready' : 'Ready'}
          hint={me()?.ready ? 'withdraw your readiness' : 'mark yourself ready'}
          enabled={allowed('player_ready') && !pending()}
          onClick={() => props.onAct('player_ready', { ready: !(me()?.ready ?? false) })}
        />
        <ActionButton
          label="Start"
          hint="only the host can start, once everyone is ready"
          enabled={allowed('game_start') && !pending()}
          onClick={() => props.onAct('game_start')}
        />
        <Show when={state().lastRoll}>
          {(roll) => (
            <span
              class="mr-1 font-display text-sm font-bold text-cream-100"
              aria-label="last dice roll"
              data-testid="last-roll"
            >
              {roll().die1} + {roll().die2} = {roll().die1 + roll().die2}
            </span>
          )}
        </Show>
        <ActionButton
          label="Roll"
          hint="roll two dice and move"
          enabled={allowed('roll_dice') && !pending()}
          onClick={() => props.onAct('roll_dice')}
        />
        <ActionButton
          label="Buy"
          hint={
            space()?.kind === 'property'
              ? `buy ${space()?.name} for ${space()?.price}`
              : 'no property to buy'
          }
          enabled={allowed('buy_property') && !pending()}
          onClick={() => props.onAct('buy_property')}
        />
        <ActionButton
          label="Decline"
          hint="skip this property (not an auction)"
          enabled={allowed('decline_buy') && !pending()}
          onClick={() => props.onAct('decline_buy')}
        />
        <ActionButton
          label="End turn"
          hint="pass the turn to the next player"
          enabled={allowed('end_turn') && !pending()}
          onClick={() => props.onAct('end_turn')}
        />
        <Show when={pending()}>
          <span role="status" class="ml-1 font-mono text-xs text-muted-400">
            waiting for server…
          </span>
        </Show>
        <Show when={gameStore.lastError()}>
          {(msg) => (
            <p role="alert" class="w-full text-center text-sm text-crimson-400">
              {msg()}
            </p>
          )}
        </Show>
        <Show when={!allowed('roll_dice') && state().phase === 'playing' && !ended()}>
          <span class="w-full text-center text-xs text-muted-500">waiting for your turn…</span>
        </Show>
      </div>
    </>
  );
}

function ActionButton(props: {
  label: string;
  hint: string;
  enabled: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      disabled={!props.enabled}
      title={props.hint}
      aria-label={`${props.label} — ${props.hint}`}
      onClick={() => void props.onClick()}
      class={
        props.enabled
          ? 'min-h-11 cursor-pointer rounded-md bg-gold-500 px-5 py-2 font-display text-sm font-bold text-felt-950 shadow-card transition-colors hover:bg-gold-400'
          : 'min-h-11 cursor-not-allowed rounded-md border border-line-subtle bg-surface-raised px-5 py-2 font-display text-sm font-bold text-muted-500 opacity-70'
      }
    >
      {props.label}
    </button>
  );
}
