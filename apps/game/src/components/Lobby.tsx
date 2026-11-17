import { createSignal, Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Button } from '@highjack/ui';
import { gameStore } from '../state/game-store.ts';
import { SERVER_URL } from '../server-url.ts';

/**
 * The lobby: a name, and the two ways in.
 */
export function Lobby(props: {
  name: string;
  setName: (v: string) => void;
  onEnter: () => void;
  onJoin: (matchId: string) => void;
  busy: boolean;
}): JSX.Element {
  const [matchId, setMatchId] = createSignal('');
  const field =
    'w-full rounded-md border border-line-strong bg-felt-950 px-3 py-2.5 text-sm text-cream-100 outline-none transition-colors placeholder:text-muted-500 focus:border-gold-500';
  return (
    <main class="flex flex-1 items-center justify-center overflow-y-auto p-6">
      <div class="w-full max-w-md border border-line-subtle bg-felt-950/70 p-8 shadow-raised">
        <p class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">
          HighJack · private tables
        </p>
        <h1 class="mb-0 mt-3 font-display text-3xl text-cream-100">Open a private table</h1>
        <p class="mb-7 mt-4 text-sm leading-relaxed text-muted-400">
          Create a table and share its id with anyone you want to play with. The server handles the
          dice, the money and the rules.
        </p>

        <label class="mb-2 block text-sm text-cream-300" for="display-name">
          Display name
        </label>
        <input
          id="display-name"
          class={field}
          maxlength={32}
          value={props.name}
          onInput={(e) => props.setName(e.currentTarget.value)}
        />
        <Button variant="primary" onClick={props.onEnter} disabled={props.busy} class="mt-4 w-full">
          {props.busy ? 'Creating match…' : 'Create a table'}
        </Button>

        <div class="my-7 flex items-center gap-3 text-xs text-muted-500">
          <span class="h-px flex-1 bg-line-subtle" />
          or join one
          <span class="h-px flex-1 bg-line-subtle" />
        </div>

        {/* Join a table someone else created, using the id they shared. */}
        <label class="mb-2 block text-sm text-cream-300" for="join-match">
          Match id
        </label>
        <div class="flex gap-2">
          <input
            id="join-match"
            class={`${field} font-mono`}
            placeholder="m_…"
            value={matchId()}
            onInput={(e) => setMatchId(e.currentTarget.value)}
          />
          <Button
            onClick={() => props.onJoin(matchId())}
            disabled={props.busy || matchId().trim() === ''}
          >
            Join
          </Button>
        </div>
        <Show when={gameStore.lastError()}>
          {(msg) => (
            <p role="alert" class="mt-4 text-sm text-crimson-400">
              {msg()}
            </p>
          )}
        </Show>
        <p class="mb-0 mt-7 border-t border-line-subtle pt-5 text-xs text-muted-500">
          Server: <code class="font-mono text-muted-400">{SERVER_URL}</code>
        </p>
      </div>
    </main>
  );
}
