import { createSignal, onCleanup, onMount, Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Badge, Button, Money, StatusDot } from '@highjack/ui';
import type { ActionType, GameConfig, GameSnapshot } from '@highjack/protocol';
import { createBoardStage, type BoardStage } from './engine/board-stage.ts';
import {
  applyEvents,
  applySnapshot,
  canBuy,
  canEndTurn,
  canRoll,
  currentPlayer,
  gameStore,
  isMyTurn,
  myHoldings,
  setLocalPlayer,
} from './state/game-store.ts';
import { createMatch, GameConnection, joinMatch, type ConnectionStatus } from './net/connection.ts';

/**
 * HighJack game client.
 *
 *   SolidJS → application UI: lobby, status, action dock, tables
 *   PixiJS  → board rendering only (engine/board-stage)
 *
 * State flows one way: server snapshot/events → game-store → (UI, stage).
 * Controls call the network layer and wait for the server's ack; a click
 * never mutates money, ownership, or turn locally.
 */

// The server is the authority; its address is deployment configuration
// (VITE_HIGHJACK_SERVER) with a same-host default that matches the port
// the server binds in development and in the certification harness.
const SERVER_PORT = import.meta.env['VITE_HIGHJACK_PORT'] ?? '8080';
const SERVER_URL =
  import.meta.env['VITE_HIGHJACK_SERVER'] ??
  (typeof window === 'undefined'
    ? `http://127.0.0.1:${SERVER_PORT}`
    : `${window.location.protocol}//${window.location.hostname}:${SERVER_PORT}`);
const WS_URL = SERVER_URL.replace(/^http/, 'ws') + '/ws';

export function App(): JSX.Element {
  let canvasRef: HTMLCanvasElement | undefined;
  let stage: BoardStage | undefined;
  const [displayName, setDisplayName] = createSignal('Ace');
  const setName = (v: string): void => {
    setDisplayName(v);
  };
  const [entering, setEntering] = createSignal(false);
  let connection: GameConnection | null = null;

  onMount(() => {
    if (!canvasRef) return;
    let handle: BoardStage | undefined;
    void createBoardStage(canvasRef, {
      reducedMotion: gameStore.reducedMotion(),
      onFps: gameStore.setFps,
      onReady: () => gameStore.setStageReady(true),
    }).then((h) => {
      handle = h;
      stage = h;
    });
    onCleanup(() => {
      connection?.close();
      void handle?.destroy();
      gameStore.setStageReady(false);
    });
  });

  const syncStage = (): void => {
    const view = gameStore.view();
    if (!stage || !view) return;
    stage.setBoard(view.spaces);
    stage.setTokens(view.players);
    stage.setTurn(view.turn);
  };

  const handlers = {
    onStatus: (status: ConnectionStatus, detail = '') => gameStore.setStatus(status, detail),
    onWelcome: () => {},
    onSnapshot: (snapshot: GameSnapshot, config: GameConfig, nextSeq: number, resync: boolean) => {
      applySnapshot(snapshot, config, nextSeq, resync);
      syncStage();
    },
    onEvents: (
      events: readonly {
        tick: number;
        event: Parameters<typeof applyEvents>[0][number]['event'];
      }[],
    ) => {
      applyEvents(events);
      syncStage();
    },
    onActionResult: (
      seq: number,
      ok: boolean,
      code?: string,
      message?: string,
      snapshot?: GameSnapshot,
    ) => {
      gameStore.setPendingAction(false);
      if (ok && snapshot) {
        // The server's own post-action state is authoritative; adopt it
        // rather than waiting for the matching event to arrive.
        const current = gameStore.view();
        applySnapshot(snapshot, current?.config ?? null, 0, false);
        syncStage();
        return;
      }
      if (!ok && seq >= 0) gameStore.setLastError(message ?? code ?? 'action rejected');
    },
  };

  const enterMatch = async (): Promise<void> => {
    setEntering(true);
    gameStore.setLastError('');
    try {
      const created = await createMatch(SERVER_URL);
      const seat = await joinMatch(SERVER_URL, created.matchId, displayName() || 'Player');
      setLocalPlayer(seat.playerId);
      connection = new GameConnection(WS_URL, created.matchId, seat.token, handlers);
      connection.connect();
      history.replaceState(null, '', `?match=${created.matchId}`);
    } catch (err) {
      gameStore.setLastError(err instanceof Error ? err.message : 'could not join');
    } finally {
      setEntering(false);
    }
  };

  const act = async (type: ActionType, extra: Record<string, unknown> = {}): Promise<void> => {
    if (!connection || gameStore.pendingAction()) return;
    gameStore.setPendingAction(true);
    gameStore.setLastError('');
    await connection.send(type, extra);
  };

  return (
    <div class="flex h-dvh flex-col overflow-hidden bg-felt-950">
      <TopBar />
      <Show
        when={gameStore.view()}
        fallback={
          <Lobby name={displayName()} setName={setName} onEnter={enterMatch} busy={entering()} />
        }
      >
        {(view) => (
          // The stage is the only element that grows; the dock stays a
          // compact strip beneath it. Keeping the dock out of the
          // absolutely-positioned stage area is what makes the mobile
          // layout usable at all.
          <>
            <div class="relative min-h-0 flex-1">
              <StageCanvas ref={(el) => (canvasRef = el)} />
              <SidePanel state={view()} />
              <StatusBanner />
            </div>
            <ActionDock state={view()} onAct={act} />
          </>
        )}
      </Show>
    </div>
  );
}

function StageCanvas(props: { ref: (el: HTMLCanvasElement) => void }): JSX.Element {
  return (
    <div class="absolute inset-0 md:left-72">
      <canvas
        ref={props.ref}
        class="h-full w-full"
        aria-label="HighJack board rendered from authoritative match state"
        role="img"
      />
    </div>
  );
}

function TopBar(): JSX.Element {
  const status = () => gameStore.status();
  const view = () => gameStore.view();
  return (
    <header class="z-10 flex items-center justify-between gap-3 border-b border-line-subtle bg-felt-900/90 px-4 py-2.5">
      <div class="flex items-center gap-3">
        <span class="font-display text-lg font-extrabold tracking-tight text-cream-100">
          High<span class="text-gold-500">Jack</span>
        </span>
        <span class="hidden rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400 sm:inline">
          board loop · v0.2.0
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

function statusLabel(status: ConnectionStatus): string {
  switch (status) {
    case 'connected':
      return 'connected';
    case 'connecting':
      return 'connecting…';
    case 'handshaking':
      return 'joining…';
    case 'reconnecting':
      return 'reconnecting…';
    case 'disconnected':
      return 'disconnected';
    case 'error':
      return 'connection error';
    default:
      return 'not connected';
  }
}

function Lobby(props: {
  name: string;
  setName: (v: string) => void;
  onEnter: () => void;
  busy: boolean;
}): JSX.Element {
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
          {props.busy ? 'Creating match…' : 'Create & join'}
        </Button>
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

function SidePanel(props: { state: NonNullable<ReturnType<typeof gameStore.view>> }): JSX.Element {
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
              classList={{ 'text-cream-100': p.seat === state().turn.currentSeat }}
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

function StatusBanner(): JSX.Element {
  const status = () => gameStore.status();
  return (
    <div class="pointer-events-none absolute inset-x-0 top-3 z-10 flex justify-center px-4">
      <Show when={status() !== 'connected'}>
        <span
          role="status"
          class="rounded-pill border border-gold-700/50 bg-felt-950/85 px-4 py-1.5 font-mono text-xs font-bold uppercase tracking-widest text-gold-400 shadow-card"
        >
          {statusLabel(status())}
        </span>
      </Show>
      <Show when={gameStore.resynced()}>
        <span class="rounded-pill border border-crimson-600/50 bg-felt-950/85 px-4 py-1.5 font-mono text-xs font-bold uppercase tracking-widest text-crimson-400 shadow-card">
          state resynced from server
        </span>
      </Show>
    </div>
  );
}

function ActionDock(props: {
  state: NonNullable<ReturnType<typeof gameStore.view>>;
  onAct: (type: ActionType, extra?: Record<string, unknown>) => Promise<void>;
}): JSX.Element {
  const state = () => props.state;
  const me = () => state().players.find((p) => p.id === state().playerId) ?? null;
  const space = () => (me() ? state().spaces[me()!.position] : undefined);
  const pending = () => gameStore.pendingAction();
  const ended = () => state().phase === 'ended';

  return (
    <nav
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

      <ActionButton
        label="Ready"
        hint="mark yourself ready"
        enabled={state().phase === 'lobby' && (me()?.ready ?? false) === false && !pending()}
        onClick={() => props.onAct('player_ready', { ready: true })}
      />
      <ActionButton
        label="Start"
        hint="only the host can start, once everyone is ready"
        enabled={state().phase === 'lobby' && (me()?.isHost ?? false) && !pending()}
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
        enabled={canRoll(state()) && !pending()}
        onClick={() => props.onAct('roll_dice')}
      />
      <ActionButton
        label="Buy"
        hint={
          space()?.kind === 'property'
            ? `buy ${space()?.name} for ${space()?.price}`
            : 'no property to buy'
        }
        enabled={canBuy(state()) && !pending()}
        onClick={() => props.onAct('buy_property')}
      />
      <ActionButton
        label="Decline"
        hint="skip this property (not an auction)"
        enabled={canBuy(state()) && !pending()}
        onClick={() => props.onAct('decline_buy')}
      />
      <ActionButton
        label="End turn"
        hint="pass the turn to the next player"
        enabled={canEndTurn(state()) && !pending()}
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
      <Show when={!isMyTurn(state()) && state().phase === 'playing' && !ended()}>
        <span class="w-full text-center text-xs text-muted-500">waiting for your turn…</span>
      </Show>
    </nav>
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
          ? 'cursor-pointer rounded-md bg-gold-500 px-5 py-2 font-display text-sm font-bold text-felt-950 shadow-card transition-colors hover:bg-gold-400'
          : 'cursor-not-allowed rounded-md border border-line-subtle bg-surface-raised px-5 py-2 font-display text-sm font-bold text-muted-500 opacity-70'
      }
    >
      {props.label}
    </button>
  );
}
