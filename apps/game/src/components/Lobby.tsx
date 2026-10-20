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
  return (
    <main class="flex flex-1 items-center justify-center overflow-y-auto p-6">
      <div class="w-full max-w-md rounded-xl border border-line-subtle bg-felt-900/60 p-6 shadow-raised">
        <h1 class="m-0 font-display text-2xl font-extrabold text-cream-100">
          Open a private table
        </h1>
        <p class="mt-2 mb-4 text-sm leading-relaxed text-muted-400">
          The server creates the match, validates the board, and owns every dice roll. Share the
          match id with a second player to play together.
        </p>
        <label class="mb-1 block text-sm font-bold text-cream-300" for="display-name">
          Display name
        </label>
        <input
          id="display-name"
          class="mb-4 w-full rounded-md border border-line-subtle bg-felt-950 px-3 py-2 text-cream-100 outline-none focus:border-gold-500"
          maxlength={32}
          value={props.name}
          onInput={(e) => props.setName(e.currentTarget.value)}
        />
        <Button onClick={props.onEnter} disabled={props.busy} class="w-full">
          {props.busy ? 'Creating match…' : 'Create a table'}
        </Button>

        <div class="my-4 flex items-center gap-3 text-xs uppercase tracking-widest text-muted-500">
          <span class="h-px flex-1 bg-line-subtle" />
          or
          <span class="h-px flex-1 bg-line-subtle" />
        </div>

        {/* Joining an existing table. The client could create a match but had no
            way to join one, so the advertised second player had to be simulated
            outside the product. */}
        <label class="mb-1 block text-xs uppercase tracking-widest text-muted-400" for="join-match">
          Match id
        </label>
        <div class="flex gap-2">
          <input
            id="join-match"
            class="w-full rounded-md border border-line-subtle bg-felt-950 px-3 py-2 font-mono text-sm text-cream-100 outline-none focus:border-gold-500"
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
            <p role="alert" class="mt-3 text-sm text-crimson-400">
              {msg()}
            </p>
          )}
        </Show>
        <p class="mt-4 text-xs text-muted-500">
          Server: <code class="font-mono text-muted-400">{SERVER_URL}</code>
        </p>
      </div>
    </main>
  );
}
