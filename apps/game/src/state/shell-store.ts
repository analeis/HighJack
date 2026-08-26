/**
 * Shell store: the tiny reactive bridge between the Pixi render layer and
 * the Solid application layer.
 *
 * The layers communicate ONLY through these signals. The stage never
 * touches UI components; components never reach into the stage's canvas.
 * This is the seam that will later carry real game state.
 */
import { createSignal } from 'solid-js';

const [fps, setFps] = createSignal(0);
const [stageReady, setStageReady] = createSignal(false);
const [reducedMotion, setReducedMotion] = createSignal(
  typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches,
);

export const shellStore = {
  fps,
  stageReady,
  reducedMotion,
  setFps,
  setStageReady,
  setReducedMotion,
};
