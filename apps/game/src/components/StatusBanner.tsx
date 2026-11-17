import { Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { gameStore } from '../state/game-store.ts';
import { statusLabel } from './labels.ts';

/**
 * The banner for connection and error state.
 */
export function StatusBanner(): JSX.Element {
  const status = () => gameStore.status();
  return (
    <div class="pointer-events-none absolute inset-x-0 top-3 z-10 flex justify-center px-4">
      <Show when={status() !== 'connected'}>
        <span
          role="status"
          class="rounded-pill border border-gold-700/50 bg-felt-950/90 px-4 py-1.5 text-xs font-semibold text-gold-400 shadow-raised"
        >
          {statusLabel(status())}
        </span>
      </Show>
      <Show when={gameStore.resynced()}>
        <span class="rounded-pill border border-crimson-600/50 bg-felt-950/90 px-4 py-1.5 text-xs font-semibold text-crimson-400 shadow-raised">
          Caught up with the server
        </span>
      </Show>
    </div>
  );
}
