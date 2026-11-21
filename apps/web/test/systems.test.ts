import { describe, expect, it } from 'vitest';
import { SYSTEMS, statusLabel, type SystemStatus } from '../src/data/systems.ts';

/**
 * The system catalog drives the status badges across the site, so it is a
 * user-facing contract: statuses must map to plain words, and no wire value
 * may leak into the interface. This is the web counterpart of the game
 * client's label tests, and it is the first unit test in apps/web — its
 * presence is also what makes the test:ts gate's web coverage meaningful.
 */
describe('statusLabel', () => {
  it('names every status in plain words', () => {
    expect(statusLabel('playable')).toBe('Playable');
    expect(statusLabel('modeled')).toBe('Modeled');
    expect(statusLabel('in-development')).toBe('In development');
    expect(statusLabel('planned')).toBe('Planned');
  });

  it('never shows the wire value', () => {
    const statuses: SystemStatus[] = ['playable', 'modeled', 'in-development', 'planned'];
    for (const status of statuses) {
      expect(statusLabel(status)).not.toBe(status);
    }
  });
});

describe('the system catalog', () => {
  it('has unique ids', () => {
    const ids = SYSTEMS.map((s) => s.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('gives every entry a status with a label and a description', () => {
    for (const system of SYSTEMS) {
      expect(statusLabel(system.status)).toBeTruthy();
      expect(system.description.length).toBeGreaterThan(0);
      expect(system.tagline.length).toBeGreaterThan(0);
    }
  });
});
