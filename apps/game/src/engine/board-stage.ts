/**
 * PixiJS board renderer.
 *
 * Renders the board from authoritative state only: the space list comes
 * from the server snapshot, tokens sit on server-reported positions, and
 * the active turn is highlighted. The client animates and draws; it never
 * decides where a token lands, what a roll was, or who owns a space.
 */
import { Application, Container, Graphics, Text, TextStyle } from 'pixi.js';
import type { Ticker } from 'pixi.js';
import type { UiPlayer, UiSpace, UiTurn } from '../state/game-store.ts';

// Token mirrors (see packages/ui/src/styles/tokens.css). Keep in sync.
const COLOR = {
  felt950: 0x070d0a,
  felt700: 0x182720,
  felt600: 0x20342b,
  gold500: 0xe6b64e,
  gold700: 0x97752a,
  cream100: 0xf4efe2,
  muted400: 0xa9baae,
  violet500: 0x9d7ff2,
  crimson500: 0xd65252,
} as const;

const PLAYER_COLORS = [0xe6b64e, 0x9d7ff2, 0x2fb89a, 0xd65252, 0x6aa9e0, 0xe08ab4];

interface Slot {
  cx: number;
  cy: number;
  tile: number;
  horizontal: boolean;
}

export interface BoardStageOptions {
  reducedMotion: boolean;
  onFps?: (fps: number) => void;
  onReady?: () => void;
}

export interface BoardStage {
  setBoard(spaces: readonly UiSpace[]): void;
  setTokens(players: readonly UiPlayer[]): void;
  setTurn(turn: UiTurn): void;
  destroy(): Promise<void>;
}

export async function createBoardStage(
  canvas: HTMLCanvasElement,
  opts: BoardStageOptions,
): Promise<BoardStage> {
  const app = new Application();
  await app.init({
    canvas,
    background: COLOR.felt950,
    antialias: true,
    resolution: Math.min(window.devicePixelRatio || 1, 2),
    autoDensity: true,
    resizeTo: canvas.parentElement ?? window,
  });

  const boardLayer = new Container();
  const tokenLayer = new Container();
  app.stage.addChild(boardLayer, tokenLayer);

  let spaces: readonly UiSpace[] = [];
  let players: readonly UiPlayer[] = [];
  let turn: UiTurn = {
    currentSeat: 0,
    phase: 'await_roll',
    round: 1,
    doublesStreak: 0,
    awardExtraRoll: false,
  };
  let slots: Slot[] = [];
  const tokens = new Map<string, Container>();

  const playerColor = (id: string | undefined): number => {
    if (!id) return COLOR.violet500;
    const idx = players.findIndex((p) => p.id === id);
    return idx >= 0
      ? (PLAYER_COLORS[idx % PLAYER_COLORS.length] ?? COLOR.violet500)
      : COLOR.violet500;
  };

  const layout = (): void => {
    boardLayer.removeChildren();
    tokenLayer.removeChildren();
    tokens.clear();
    const w = app.screen.width;
    const h = app.screen.height;
    if (spaces.length === 0) return;

    const perSide = Math.max(1, Math.ceil(spaces.length / 4));
    const side = Math.max(140, Math.min(w * 0.94, h * 0.94));
    const tile = side / perSide;
    const thickness = tile * 0.74;
    const x0 = w / 2 - side / 2;
    const y0 = h / 2 - side / 2;

    slots = spaces.map((space, i) => {
      const sideIndex = Math.floor(i / perSide);
      const posInSide = i % perSide;
      let cx = 0;
      let cy = 0;
      let horizontal = true;
      if (sideIndex === 0) {
        cx = x0 + (posInSide + 0.5) * tile;
        cy = y0;
      } else if (sideIndex === 1) {
        cx = x0 + side;
        cy = y0 + (posInSide + 0.5) * tile;
        horizontal = false;
      } else if (sideIndex === 2) {
        cx = x0 + side - (posInSide + 0.5) * tile;
        cy = y0 + side;
      } else {
        cx = x0;
        cy = y0 + side - (posInSide + 0.5) * tile;
        horizontal = false;
      }
      return { cx, cy, tile, horizontal };
    });

    spaces.forEach((space, i) => {
      const slot = slots[i];
      if (!slot) return;
      const w2 = slot.horizontal ? tile : thickness;
      const h2 = slot.horizontal ? thickness : tile;
      const active =
        players.find((p) => p.seat === turn.currentSeat)?.position === i &&
        turn.phase !== 'turn_over';
      const fill = space.owned
        ? playerColor(space.owner)
        : space.kind === 'go'
          ? COLOR.gold700
          : space.kind === 'tax'
            ? COLOR.crimson500
            : space.kind === 'neutral'
              ? COLOR.felt600
              : COLOR.felt700;
      const g = new Graphics()
        .roundRect(-w2 / 2 + 1, -h2 / 2 + 1, w2 - 2, h2 - 2, 5)
        .fill(fill)
        .stroke({
          width: active ? 3 : 1.5,
          color: active ? COLOR.cream100 : space.kind === 'go' ? COLOR.gold500 : 0x2c4437,
          alpha: 0.9,
        });
      g.position.set(slot.cx, slot.cy);
      boardLayer.addChild(g);

      const text =
        space.kind === 'property' ? space.price.toLocaleString('en-US') : space.kind.toUpperCase();
      const label = new Text({
        text,
        style: new TextStyle({
          fontFamily: 'Space Grotesk Variable, system-ui, sans-serif',
          fontSize: Math.max(9, Math.min(13, tile * 0.24)),
          fontWeight: '700',
          fill: space.owned ? COLOR.felt950 : COLOR.muted400,
        }),
      });
      label.anchor.set(0.5);
      label.position.set(slot.cx, slot.cy);
      boardLayer.addChild(label);
    });

    players.forEach((player, idx) => {
      const slot = slots[player.position];
      if (!slot) return;
      const node = new Container();
      const disc = new Graphics()
        .circle(0, 0, slot.tile * 0.17)
        .fill(PLAYER_COLORS[idx % PLAYER_COLORS.length])
        .stroke({ width: 2, color: COLOR.cream100 });
      const initial = new Text({
        text: player.name.slice(0, 1).toUpperCase(),
        style: new TextStyle({
          fontFamily: 'Space Grotesk Variable, system-ui, sans-serif',
          fontSize: Math.max(9, slot.tile * 0.2),
          fontWeight: '800',
          fill: COLOR.felt950,
        }),
      });
      initial.anchor.set(0.5);
      node.addChild(disc, initial);
      const k = (idx % 4) - 1.5;
      const off = slot.tile * 0.16;
      node.position.set(
        slot.horizontal ? slot.cx + k * off : slot.cx,
        slot.horizontal ? slot.cy : slot.cy + k * off,
      );
      if (player.status !== 'active') node.alpha = 0.35;
      tokenLayer.addChild(node);
      tokens.set(player.id, node);
    });
  };

  const onResize = (): void => layout();
  window.addEventListener('resize', onResize);

  if (!opts.reducedMotion) {
    app.ticker.add((ticker: Ticker) => {
      const pulse = 1 + Math.sin(ticker.lastTime / 320) * 0.06;
      for (const player of players) {
        const node = tokens.get(player.id);
        if (!node) continue;
        const isTurn =
          player.seat === turn.currentSeat &&
          player.status === 'active' &&
          turn.phase !== 'turn_over';
        node.scale.set(isTurn ? pulse : 1);
      }
      if (opts.onFps && ticker.FPS !== 0) opts.onFps(Math.round(ticker.FPS));
    });
  } else if (opts.onFps) {
    opts.onFps(0);
  }

  opts.onReady?.();

  return {
    setBoard(next) {
      spaces = next;
      layout();
    },
    setTokens(next) {
      players = next;
      layout();
    },
    setTurn(next) {
      turn = next;
      layout();
    },
    async destroy() {
      window.removeEventListener('resize', onResize);
      await app.destroy(true, { children: true });
    },
  };
}
