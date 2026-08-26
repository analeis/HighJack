import { onCleanup, onMount, Show } from 'solid-js';
import type { JSX } from 'solid-js';
import { Badge, Money, StatusDot } from '@highjack/ui';
import { DEFAULT_CONFIG } from '@highjack/protocol';
import { createStage, type StageHandle } from './engine/pixi-stage';
import { shellStore } from './state/shell-store';

/**
 * HighJack game-shell concept screen.
 *
 * Demonstrates the intended client composition:
 *
 *   SolidJS  → application UI: top bar, side panels, action dock
 *   PixiJS   → the render stage (this canvas), owned by engine/pixi-stage
 *
 * The layers communicate only through shellStore signals. No gameplay is
 * implemented; disabled controls say so explicitly.
 */
export function App(): JSX.Element {
  let canvasRef: HTMLCanvasElement | undefined;

  onMount(() => {
    if (!canvasRef) return;
    let handle: StageHandle | undefined;
    createStage(canvasRef, {
      reducedMotion: shellStore.reducedMotion(),
      onFps: shellStore.setFps,
      onReady: () => shellStore.setStageReady(true),
    }).then((h) => {
      handle = h;
    });
    onCleanup(() => {
      void handle?.destroy();
      shellStore.setStageReady(false);
    });
  });

  return (
    <div class="flex h-dvh flex-col overflow-hidden bg-felt-950">
      <TopBar />
      <div class="relative flex min-h-0 flex-1">
        <StageCanvas ref={(el) => (canvasRef = el)} />
        <SidePanel />
        <ConceptBadge />
      </div>
      <ActionDock />
    </div>
  );
}

function StageCanvas(props: { ref: (el: HTMLCanvasElement) => void }): JSX.Element {
  return (
    <div class="absolute inset-0 md:left-64">
      <canvas
        ref={props.ref}
        class="h-full w-full"
        aria-label="HighJack game board concept render"
        role="img"
      />
    </div>
  );
}

function TopBar(): JSX.Element {
  return (
    <header class="z-10 flex items-center justify-between gap-3 border-b border-line-subtle bg-felt-900/90 px-4 py-2.5">
      <div class="flex items-center gap-3">
        <span class="font-display text-lg font-extrabold tracking-tight text-cream-100">
          High<span class="text-gold-500">Jack</span>
        </span>
        <span class="hidden rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400 sm:inline">
          game-shell · v0.1.0
        </span>
      </div>
      <div class="flex items-center gap-2">
        <StatusDot status="soon" label="Offline concept demo — not connected" />
        <span class="hidden text-xs text-muted-500 sm:inline">offline concept</span>
        <span
          class="rounded-md border border-line-subtle bg-surface-raised px-2 py-0.5 font-mono text-xs text-muted-400"
          title="frames per second of the render stage"
        >
          {shellStore.stageReady() ? `${shellStore.fps()} fps` : '…'}
        </span>
      </div>
    </header>
  );
}

function SidePanel(): JSX.Element {
  const cfg = DEFAULT_CONFIG;
  return (
    <aside
      aria-label="Match information"
      class="absolute inset-y-0 left-0 z-10 hidden w-64 flex-col gap-4 overflow-y-auto border-r border-line-subtle bg-felt-900/95 p-4 md:flex"
    >
      <section>
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">Shell status</h2>
        <ul class="mb-0 mt-2 list-none space-y-1.5 p-0 text-sm text-muted-400">
          <li class="flex items-center justify-between">
            Render stage
            <Show when={shellStore.stageReady()} fallback={<Badge tone="neutral">booting</Badge>}>
              <Badge tone="teal">live</Badge>
            </Show>
          </li>
          <li class="flex items-center justify-between">
            Connection <span class="text-muted-500">none (demo)</span>
          </li>
          <li class="flex items-center justify-between">
            Engine link <span class="text-muted-500">v0.2</span>
          </li>
        </ul>
      </section>

      <section>
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">
          Ruleset preview
        </h2>
        <p class="mt-1 mb-0 text-xs leading-relaxed text-muted-500">
          Default GameConfig that a real match would load:
        </p>
        <dl class="m-0 mt-2 space-y-1.5 text-sm">
          <Row label="Players" value={`${cfg.playerCount.min}–${cfg.playerCount.max}`} />
          <Row label="Starting chips" value={<Money amount={cfg.startingMoney} />} />
          <Row label="Trading" value={cfg.trading.enabled ? 'on' : 'off'} />
          <Row label="Auctions" value={cfg.auctions.enabled ? 'on' : 'off'} />
          <Row label="Treasure cards" value={cfg.cards.enabled ? 'on' : 'off'} />
          <Row label="Victory" value="last standing" />
        </dl>
      </section>

      <section class="mt-auto">
        <h2 class="m-0 font-mono text-xs uppercase tracking-widest text-gold-500">Layers</h2>
        <p class="mb-0 mt-2 text-xs leading-relaxed text-muted-500">
          Panels and menus are SolidJS. The board is a PixiJS stage behind this panel. They share
          nothing but the shell store — the seam real gameplay will flow through.
        </p>
      </section>
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

function ConceptBadge(): JSX.Element {
  return (
    <div class="pointer-events-none absolute inset-x-0 top-3 z-10 flex justify-center px-4">
      <span class="rounded-pill border border-gold-700/50 bg-felt-950/80 px-4 py-1.5 font-mono text-xs font-bold uppercase tracking-widest text-gold-400 shadow-card backdrop-blur">
        concept preview — gameplay arrives in v0.2
      </span>
    </div>
  );
}

const DOCK_ACTIONS = [
  { id: 'roll', label: 'Roll', hint: 'dice arrive with v0.2' },
  { id: 'buy', label: 'Buy', hint: 'property system not implemented yet' },
  { id: 'trade', label: 'Trade', hint: 'trading desk planned' },
  { id: 'wager', label: 'Wager', hint: 'gambling tables planned' },
];

function ActionDock(): JSX.Element {
  return (
    <nav
      aria-label="Planned match actions"
      class="z-10 flex items-center justify-center gap-2 border-t border-line-subtle bg-felt-900/90 px-4 py-3"
    >
      {DOCK_ACTIONS.map((action) => (
        <button
          type="button"
          disabled
          title={`${action.label}: ${action.hint}`}
          aria-label={`${action.label} — ${action.hint}`}
          class="cursor-not-allowed rounded-md border border-line-subtle bg-surface-raised px-5 py-2 font-display text-sm font-bold text-muted-500 opacity-70"
        >
          {action.label}
        </button>
      ))}
    </nav>
  );
}
