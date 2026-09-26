import { createEffect, createSignal, onCleanup, onMount, Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Badge, Button, Money, StatusDot } from '@highjack/ui';
import type { ActionType, GameConfig, GameSnapshot } from '@highjack/protocol';
import { createBoardStage, type BoardStage } from './engine/board-stage.ts';
import {
  applyEvents,
  applyLegalActions,
  applySnapshot,
  currentPlayer,
  gameStore,
  myHoldings,
  setLocalPlayer,
} from './state/game-store.ts';
import type { GameView } from './state/game-store.ts';
import {
  GameConnection,
  createMatch,
  joinMatch,
  readStoredSeat,
  rememberSeat,
  type ConnectionStatus,
} from './net/connection.ts';

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
    onLegalActions: (actions: readonly ActionType[]) => {
      applyLegalActions(actions);
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

  /**
   * Claim a seat and connect.
   *
   * The seat is persisted so a reload rejoins the same table instead of orphaning
   * it. The match id was previously written into the query string and never read
   * back, and the token was never stored at all — so a refresh created a *new*
   * match and left the player with a dead one. That made the advertised two-player
   * flow unreachable through the product: the only way in was a raw WebSocket in
   * the test suite.
   */
  const openSeat = async (matchId: string, name: string): Promise<void> => {
    const seat = await joinMatch(SERVER_URL, matchId, name);
    setLocalPlayer(seat.playerId);
    rememberSeat(matchId, seat.token, seat.playerId);
    connection = new GameConnection(WS_URL, matchId, seat.token, handlers);
    connection.connect();
    history.replaceState(null, '', `?match=${encodeURIComponent(matchId)}`);
  };

  const enterMatch = async (): Promise<void> => {
    setEntering(true);
    gameStore.setLastError('');
    try {
      const created = await createMatch(SERVER_URL);
      await openSeat(created.matchId, displayName() || 'Player');
    } catch (err) {
      gameStore.setLastError(err instanceof Error ? err.message : 'could not join');
    } finally {
      setEntering(false);
    }
  };

  const joinExisting = async (matchId: string): Promise<void> => {
    setEntering(true);
    gameStore.setLastError('');
    try {
      await openSeat(matchId.trim(), displayName() || 'Player');
    } catch (err) {
      gameStore.setLastError(err instanceof Error ? err.message : 'could not join that match');
    } finally {
      setEntering(false);
    }
  };

  // A stored seat, or a match id in the URL, means this browser already belongs
  // to a table. Rejoin it rather than offering to create another.
  onMount(() => {
    const stored = readStoredSeat();
    const fromUrl = new URLSearchParams(window.location.search).get('match');
    const matchId = stored?.matchId ?? fromUrl;
    if (!matchId) return;
    if (stored) {
      setLocalPlayer(stored.playerId);
      connection = new GameConnection(WS_URL, stored.matchId, stored.token, handlers);
      connection.connect();
      return;
    }
    // Someone shared a link: claim a seat at their table.
    void joinExisting(matchId);
  });

  const act = async (type: ActionType, extra: Record<string, unknown> = {}): Promise<void> => {
    if (!connection || gameStore.pendingAction()) return;
    // Never act on a socket that is not open. The send used to be attempted
    // anyway and its failure ignored, so the click vanished with no feedback.
    if (gameStore.status() !== 'connected') {
      gameStore.setLastError('not connected — reconnecting');
      return;
    }
    gameStore.setPendingAction(true);
    gameStore.setLastError('');
    const sent = await connection.send(type, extra);
    if (!sent) {
      // The action never reached the server, so nothing is in flight. Clearing
      // here is what stops one dropped connection from wedging the dock: the flag
      // used to be cleared only by an acknowledgement, and a socket that closes
      // mid-action never sends one, leaving every button disabled behind a
      // permanent "waiting for server…" until a reload — which then discarded the
      // player's seat.
      gameStore.setPendingAction(false);
      gameStore.setLastError('action could not be sent');
    }
  };

  // A connection that drops leaves nothing in flight, so the pending flag must not
  // survive it. Clearing on status change rather than on close covers a failed
  // send, a refused reconnect and a deliberate teardown alike.
  createEffect(() => {
    const status = gameStore.status();
    if (status === 'reconnecting' || status === 'disconnected' || status === 'error') {
      gameStore.setPendingAction(false);
    }
  });

  // Focus handoff at the lobby -> board transition. Removing the lobby unmounts
  // whatever had focus, which dropped a keyboard or screen-reader user at the top of
  // the document with no indication that the view had changed; focus then sat on
  // <body> and the next Tab reached the browser chrome. The stage carries a label
  // describing where the user has arrived.
  let stageEl: HTMLDivElement | undefined;
  createEffect(() => {
    if (gameStore.view() && stageEl) stageEl.focus();
  });

  return (
    <div class="flex h-dvh flex-col overflow-hidden bg-felt-950">
      <Announcer />
      <TopBar />
      <Show
        when={gameStore.view()}
        fallback={
          <Lobby
            name={displayName()}
            setName={setName}
            onEnter={enterMatch}
            onJoin={joinExisting}
            busy={entering()}
          />
        }
      >
        {(view) => (
          // The stage is the only element that grows; the dock stays a
          // compact strip beneath it. Keeping the dock out of the
          // absolutely-positioned stage area is what makes the mobile
          // layout usable at all.
          <>
            <div
              ref={stageEl}
              tabIndex={-1}
              aria-label="Match board and actions"
              class="relative min-h-0 flex-1"
            >
              <StageCanvas ref={(el) => (canvasRef = el)} />
              <BoardTextEquivalent />
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
      {/*
        The canvas is decorative and hidden from assistive technology. Its previous
        label described the rendering method — "rendered from authoritative match
        state" — which told a screen-reader user nothing about the board: no dice,
        no properties, no prices, no positions, no turn. BoardTextEquivalent is the
        accessible representation, and it carries the same state.
      */}
      <canvas ref={props.ref} class="h-full w-full" aria-hidden="true" />
    </div>
  );
}

/**
 * The board, in words.
 *
 * A canvas is opaque to assistive technology, so the board needs a text equivalent
 * carrying the same authoritative state. It is visually hidden but present in the
 * accessibility tree, and it is a real structure — headings and tables — rather than
 * one long string, so a screen-reader user can navigate to the part they want
 * instead of hearing the whole board on every change.
 */
function BoardTextEquivalent(): JSX.Element {
  const view = gameStore.view();
  return (
    <div class="sr-only" aria-label="Board state">
      <Show when={view} fallback={<p>No active match.</p>}>
        {(v) => {
          const board = v();
          const active = board.players.find(
            (p) => p.seat === board.turn.currentSeat && p.status === 'active',
          );
          const roll = board.lastRoll;
          return (
            <>
              <h2>Match {board.matchId}</h2>
              <p>
                {describePhase(board)} Round {board.turn.round}.{' '}
                {active
                  ? `${active.name} to act${board.playerId === active.id ? ' — that is you' : ''}.`
                  : 'No player has the turn.'}{' '}
                {roll
                  ? `Last roll ${roll.die1} and ${roll.die2} by ${
                      board.players.find((p) => p.id === roll.byId)?.name ?? 'unknown'
                    }.`
                  : 'No dice rolled yet.'}
                {board.winnerId
                  ? ` Winner: ${board.players.find((p) => p.id === board.winnerId)?.name ?? board.winnerId}.`
                  : ''}
              </p>

              <h3>Players</h3>
              <table>
                <caption>Players, chips and positions</caption>
                <thead>
                  <tr>
                    <th scope="col">Seat</th>
                    <th scope="col">Player</th>
                    <th scope="col">Chips</th>
                    <th scope="col">On</th>
                    <th scope="col">State</th>
                  </tr>
                </thead>
                <tbody>
                  {board.players.map((p) => (
                    <tr>
                      <td>{p.seat + 1}</td>
                      <td>
                        {p.name}
                        {p.id === board.playerId ? ' (you)' : ''}
                      </td>
                      <td>{p.money.toLocaleString('en-US')}</td>
                      <td>{board.spaces[p.position]?.name ?? 'off board'}</td>
                      <td>
                        {[
                          p.status !== 'active' ? p.status : null,
                          p.isHost ? 'host' : null,
                          p.ready ? 'ready' : 'not ready',
                        ]
                          .filter(Boolean)
                          .join(', ')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <h3>Spaces</h3>
              <table>
                <caption>Spaces in board order, with price, rent and owner</caption>
                <thead>
                  <tr>
                    <th scope="col">Space</th>
                    <th scope="col">Price</th>
                    <th scope="col">Rent</th>
                    <th scope="col">Owner</th>
                  </tr>
                </thead>
                <tbody>
                  {board.spaces.map((space) => (
                    <tr>
                      <td>
                        {space.name}
                        {space.level > 0 ? ` (level ${space.level})` : ''}
                      </td>
                      <td>
                        {space.kind === 'property' ? space.price.toLocaleString('en-US') : '—'}
                      </td>
                      <td>{space.rent.toLocaleString('en-US')}</td>
                      <td>
                        {space.owned
                          ? (board.players.find((p) => p.id === space.owner)?.name ?? 'unknown')
                          : 'unowned'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          );
        }}
      </Show>
    </div>
  );
}

function describePhase(v: GameView): string {
  switch (v.phase) {
    case 'lobby':
      return 'Lobby, waiting for players.';
    case 'playing':
      return v.turn.phase === 'await_buy_decision'
        ? 'In play, deciding whether to buy.'
        : v.turn.phase === 'await_roll'
          ? 'In play, rolling.'
          : 'In play.';
    case 'ended':
      return `Finished${v.endReason ? `: ${v.endReason}` : ''}.`;
    case 'interrupted':
      return 'Interrupted.';
    default:
      return '';
  }
}

/**
 * A live region that is mounted once and never removed.
 *
 * The dock and the side panel each carried `role="status"` on elements that were
 * mounted and unmounted with the state they reported. A live region has to be in the
 * accessibility tree *before* its content changes, so a region that arrives with the
 * message it is announcing usually says nothing at all. This one is always present;
 * only its text changes.
 */
function Announcer(): JSX.Element {
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
