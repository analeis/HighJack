import { describe, expect, it } from 'vitest';
import { endReasonLabel, playerStatusLabel, statusLabel } from '../src/components/labels.ts';

/**
 * The vocabulary contract. Every wire value a player can see has to pass
 * through one of these functions. Tests fail on a raw enum leaking back into
 * the interface — the defect the v0.2.15 patch fixed across six components.
 */
describe('statusLabel', () => {
  it('names every connection state in player words', () => {
    expect(statusLabel('idle')).toBe('Not connected');
    expect(statusLabel('connecting')).toBe('Connecting…');
    expect(statusLabel('handshaking')).toBe('Taking your seat…');
    expect(statusLabel('connected')).toBe('Connected');
    expect(statusLabel('reconnecting')).toBe('Reconnecting…');
    expect(statusLabel('disconnected')).toBe('Offline');
    expect(statusLabel('error')).toBe('Connection lost');
  });

  it('never shows the wire value itself', () => {
    const states = [
      'idle',
      'connecting',
      'handshaking',
      'connected',
      'reconnecting',
      'disconnected',
      'error',
    ] as const;
    for (const state of states) {
      expect(statusLabel(state)).not.toBe(state);
    }
  });

  it('falls back to a readable phrase for an unknown state', () => {
    expect(statusLabel('future-state' as 'idle')).toBe('Not connected');
  });
});

describe('playerStatusLabel', () => {
  it('uses the words players use', () => {
    expect(playerStatusLabel('active')).toBe('Playing');
    expect(playerStatusLabel('eliminated')).toBe('Out');
    expect(playerStatusLabel('disconnected')).toBe('Away');
  });

  it('never shows an engine status', () => {
    for (const status of ['active', 'eliminated', 'disconnected']) {
      expect(playerStatusLabel(status)).not.toBe(status);
    }
  });
});

describe('endReasonLabel', () => {
  it('explains why a match finished', () => {
    expect(endReasonLabel('last_standing')).toBe('last player standing');
    expect(endReasonLabel('target_wealth')).toBe('target wealth reached');
    expect(endReasonLabel('round_limit')).toBe('round limit reached');
    expect(endReasonLabel('insufficient_players')).toBe('not enough players');
  });

  it('never shows the reason code', () => {
    for (const reason of ['last_standing', 'target_wealth', 'round_limit']) {
      expect(endReasonLabel(reason)).not.toBe(reason);
    }
  });
});
