import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { StatusDot } from '@highjack/ui';
import { gameStore } from '../state/game-store.ts';
import { statusLabel } from './labels.ts';

/** The header: identity, match, and connection state. */
export function TopBar(): JSX.Element {
  const status = () => gameStore.status();
  const view = () => gameStore.view();
  return (
    <header class="z-10 flex items-center justify-between gap-3 border-b border-line-subtle bg-felt-950/95 px-4 py-3 sm:px-6">
      <div class="flex min-w-0 items-center gap-3">
        <span class="font-display text-lg font-semibold tracking-tight text-cream-100">
          High<span class="text-gold-500">Jack</span>
        </span>
        {/* Visible on every viewport: a player reporting a problem should be able
            to say which build — and which table — they are on without widening
            the screen. */}
        <span class="font-mono text-xs text-muted-500">v{__APP_VERSION__}</span>
        <Show when={view()}>
          {(v) => (
            <span class="max-w-[9rem] truncate font-mono text-xs text-muted-500">
              match {v().matchId}
            </span>
          )}
        </Show>
      </div>
      <div class="flex items-center gap-2">
        <StatusDot
          status={
            status() === 'connected' ? 'live' : status() === 'reconnecting' ? 'soon' : 'planned'
          }
          label={`Connection: ${statusLabel(status())}`}
        />
        <span class="text-xs text-muted-500">{statusLabel(status())}</span>
        <Show when={import.meta.env.DEV}>
          <span class="hidden font-mono text-xs text-muted-500 lg:inline">
            {gameStore.stageReady() ? `${gameStore.fps()} fps` : '…'}
          </span>
        </Show>
      </div>
    </header>
  );
}
