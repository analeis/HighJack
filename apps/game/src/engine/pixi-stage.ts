/**
 * The PixiJS render layer.
 *
 * This module owns the canvas and nothing else. It renders the HighJack
 * game-shell concept scene: a board ring, floating chips and a dealt card.
 * It is a *composition study* — deliberately not gameplay. All state flows
 * out through callbacks wired to the shell store by App.tsx.
 */
import { Application, Container, Graphics, Text, TextStyle } from 'pixi.js';
import type { Ticker } from 'pixi.js';

// Token mirrors (see packages/ui/src/styles/tokens.css). Canvas code reads
// literals; keep in sync when tokens change.
const COLOR = {
  felt950: 0x070d0a,
  felt900: 0x0b1310,
  felt800: 0x111c17,
  felt700: 0x182720,
  felt600: 0x20342b,
  gold500: 0xe6b64e,
  gold700: 0x97752a,
  cream100: 0xf4efe2,
  muted400: 0xa9baae,
  violet500: 0x9d7ff2,
} as const;

export interface StageHandle {
  destroy(): Promise<void>;
}

export interface StageOptions {
  reducedMotion: boolean;
  onFps?: (fps: number) => void;
  onReady?: () => void;
}

export async function createStage(
  canvas: HTMLCanvasElement,
  opts: StageOptions,
): Promise<StageHandle> {
  const app = new Application();
  await app.init({
    canvas,
    background: COLOR.felt950,
    antialias: true,
    resolution: Math.min(window.devicePixelRatio || 1, 2),
    autoDensity: true,
    resizeTo: canvas.parentElement ?? window,
  });

  // ---- board ring ----------------------------------------------------------
  const board = new Container();
  app.stage.addChild(board);

  const layoutBoard = () => {
    board.removeChildren();
    const w = app.screen.width;
    const h = app.screen.height;
    const size = Math.min(w, h);
    const margin = size * 0.08;
    const side = size - margin * 2;
    const tileCount = 13; // per side including corners
    const tileLen = side / tileCount;
    const thickness = tileLen * 0.72;

    let index = 0;
    const place = (x: number, y: number, horizontal: boolean) => {
      const isCorner = index % (tileCount - 1) === 0;
      const isEvent = !isCorner && index % 5 === 3;
      const g = new Graphics()
        .roundRect(
          -(horizontal ? tileLen : thickness) / 2,
          -(horizontal ? thickness : tileLen) / 2,
          horizontal ? tileLen : thickness,
          horizontal ? thickness : tileLen,
          6,
        )
        .fill(
          isCorner
            ? COLOR.gold700
            : isEvent
              ? COLOR.violet500
              : index % 2 === 0
                ? COLOR.felt700
                : COLOR.felt600,
        )
        .stroke({
          width: 1.5,
          color: isCorner ? COLOR.gold500 : 0x2c4437,
          alpha: isCorner ? 0.9 : 0.5,
        });
      g.position.set(x, y);
      if (!opts.reducedMotion) {
        g.alpha = 0.92;
      }
      board.addChild(g);
      index++;
    };

    const x0 = w / 2 - side / 2;
    const y0 = h / 2 - side / 2;
    for (let i = 0; i < tileCount; i++) place(x0 + i * tileLen + tileLen / 2, y0, true); // top
    for (let i = 1; i < tileCount; i++) place(x0 + side, y0 + i * tileLen + tileLen / 2, false); // right
    for (let i = 1; i < tileCount; i++)
      place(x0 + side - i * tileLen - tileLen / 2, y0 + side, true); // bottom
    for (let i = 1; i < tileCount - 1; i++) place(x0, y0 + side - i * tileLen - tileLen / 2, false); // left
  };
  layoutBoard();

  // ---- centerpiece: dealt card + chips --------------------------------------
  const center = new Container();
  app.stage.addChild(center);

  const card = new Container();
  const cardBody = new Graphics()
    .roundRect(-46, -64, 92, 128, 10)
    .fill(COLOR.cream100)
    .stroke({ width: 2, color: COLOR.gold500 });
  const cardArrow = new Graphics()
    .moveTo(0, 34)
    .lineTo(-20, 6)
    .lineTo(-8, 6)
    .lineTo(-8, -26)
    .lineTo(8, -26)
    .lineTo(8, 6)
    .lineTo(20, 6)
    .closePath()
    .fill(COLOR.felt900);
  card.addChild(cardBody, cardArrow);

  const chipA = new Graphics()
    .circle(0, 0, 22)
    .fill(COLOR.gold500)
    .circle(0, 0, 14)
    .fill(COLOR.felt800)
    .stroke({ width: 2, color: COLOR.cream100 });
  const chipB = new Graphics()
    .circle(0, 0, 18)
    .fill(COLOR.violet500)
    .circle(0, 0, 11)
    .fill(COLOR.felt800)
    .stroke({ width: 2, color: COLOR.cream100 });

  center.addChild(chipB, chipA, card);

  const label = new Text({
    text: 'GAME-SHELL CONCEPT · NO GAMEPLAY YET',
    style: new TextStyle({
      fontFamily: 'Space Grotesk Variable, system-ui, sans-serif',
      fontSize: 12,
      fontWeight: '700',
      letterSpacing: 3,
      fill: COLOR.muted400,
    }),
  });
  label.anchor.set(0.5);
  app.stage.addChild(label);

  const layoutCenter = () => {
    const cx = app.screen.width / 2;
    const cy = app.screen.height / 2;
    card.position.set(cx, cy - 30);
    chipA.position.set(cx - 90, cy + 60);
    chipB.position.set(cx + 84, cy + 48);
    label.position.set(cx, cy + Math.min(app.screen.height, app.screen.width) / 2 - 24);
  };
  layoutCenter();

  // ---- motion ----------------------------------------------------------------
  if (!opts.reducedMotion) {
    let time = 0;
    const tick = (ticker: Ticker) => {
      time += ticker.deltaMS / 1000;
      card.rotation = Math.sin(time * 0.8) * 0.06 - 0.06;
      card.position.y -= Math.sin(time * 1.4) * 0.35;
      chipA.position.y += Math.sin(time * 1.1 + 1) * 0.25;
      chipB.position.y += Math.sin(time * 1.7 + 2) * 0.2;
      if (opts.onFps && ticker.FPS !== 0) {
        opts.onFps(Math.round(ticker.FPS));
      }
    };
    app.ticker.add(tick);
  } else if (opts.onFps) {
    opts.onFps(0); // static scene: no ticker
  }

  const onResize = () => {
    layoutBoard();
    layoutCenter();
  };
  window.addEventListener('resize', onResize);

  opts.onReady?.();

  return {
    async destroy() {
      window.removeEventListener('resize', onResize);
      await app.destroy(true, { children: true });
    },
  };
}
