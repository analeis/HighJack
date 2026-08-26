import { describe, expect, it } from 'vitest';
import { createRoot } from 'solid-js';
import { shellStore } from '../src/state/shell-store';

describe('shell store', () => {
  it('exposes writable fps and readiness signals', () => {
    createRoot((dispose) => {
      expect(shellStore.stageReady()).toBe(false);
      shellStore.setStageReady(true);
      expect(shellStore.stageReady()).toBe(true);

      shellStore.setFps(60);
      expect(shellStore.fps()).toBe(60);

      // Reduced motion defaults to the media query value; setting is allowed
      // (e.g. an in-game accessibility toggle overriding it).
      const current = shellStore.reducedMotion();
      shellStore.setReducedMotion(!current);
      expect(shellStore.reducedMotion()).toBe(!current);
      dispose();
    });
  });
});
