import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { StatusDot } from '@highjack/ui';
import { gameStore } from '../state/game-store.ts';
import { statusLabel } from './labels.ts';

/**
 * The header: the product mark, the release, the match, and the connection state.
 */
export function TopBar(): JSX.Element {
  const status = () => gameStore.status();
  const view = () => gameStore.view();
  return (
    <header class="z-10 flex items-center justify-between gap-3 border-b border-line-subtle bg-felt-900/90 px-4 py-2.5">
      <div class="flex items-center gap-3">
        <span class="font-display text-lg font-extrabold tracking-tight text-cream-100">
          High<span class="text-gold-500">Jack</span>
        </span>
        {/* The release this client is. Visible on every viewport: it is short,
            and a player who needs to report a problem should not have to reach
            a wider screen to say which build they are on. */}
        <span class="rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400">
          <span class="hidden sm:inline">board loop · </span>v{__APP_VERSION__}
        </span>
        <Show when={view()}>
          {(v) => (
            <span class="max-w-[9rem] truncate rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400">
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
          label={`connection ${status()}`}
        />
        <span class="hidden text-xs text-muted-500 sm:inline">{statusLabel(status())}</span>
        <span
          class="rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400"
          title="frames per second of the render stage"
        >
          {gameStore.stageReady() ? `${gameStore.fps()} fps` : '…'}
        </span>
      </div>
    </header>
  );
}
