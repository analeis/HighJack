import { createEffect, createSignal, onCleanup, onMount, Show } from 'solid-js';
import type { JSX } from 'solid-js';
import type { ActionType, GameConfig, GameSnapshot } from '@highjack/protocol';
import { createBoardStage, type BoardStage } from './engine/board-stage.ts';
import {
  applyEvents,
  applyLegalActions,
  applySnapshot,
  gameStore,
  setLocalPlayer,
} from './state/game-store.ts';
import {
  GameConnection,
  createMatch,
  forgetSeat,
  joinMatch,
  readStoredSeat,
  rememberSeat,
  type ConnectionStatus,
} from './net/connection.ts';
import { ActionDock } from './components/ActionDock.tsx';
import { Announcer } from './components/Announcer.tsx';
import { BoardTextEquivalent, StageCanvas } from './components/BoardView.tsx';
import { Lobby } from './components/Lobby.tsx';
import { SidePanel } from './components/SidePanel.tsx';
import { StatusBanner } from './components/StatusBanner.tsx';
import { TopBar } from './components/TopBar.tsx';
import { SERVER_URL, WS_URL } from './server-url.ts';

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
      // A finished match has no seat to return to. The stored seat is a bearer
      // credential that never expires, and it used to be kept for the life of the
      // browser, so a reload kept sending the player back to a table that had
      // already ended.
      if (snapshot.phase === 'ended') forgetSeat(snapshot.matchId);
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
      // Leaving is a deliberate act, so the stored credential is dropped: a
      // reload then offers a fresh table rather than rejoining one this player
      // has walked away from.
      const view = gameStore.view();
      for (const entry of events) {
        if (
          entry.event.type === 'player_left' &&
          entry.event.playerId === view?.playerId &&
          view.matchId !== ''
        ) {
          forgetSeat(view.matchId);
        }
      }
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
      if (!ok && seq < 0) {
        // A handshake-level refusal: the server rejected the *match or the
        // token*, not an action. The seat cannot be used again, so it is
        // dropped rather than retried on every reload — which is what would
        // otherwise happen, because a rejected seat was never forgotten.
        if (message === 'unknown match' || message === 'invalid join token') {
          forgetSeat();
          gameStore.setLastError('that table is no longer available — create a new one');
        }
      }
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
              ref={(el) => (stageEl = el)}
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
