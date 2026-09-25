import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  ACTION_TYPES,
  DEFAULT_CONFIG,
  ERROR_CODES,
  EVENT_TYPES,
  PROTOCOL_MAJOR_VERSION,
  SCHEMA_VERSION,
  canonicalJson,
  isActionMessage,
  isCatchupMessage,
  isClientMessage,
  isErrorMessage,
  isEventMessage,
  isGameAction,
  isGameEvent,
  isGameSnapshot,
  isHelloMessage,
  isPongMessage,
  isServerMessage,
  isSnapshotMessage,
  isWelcomeMessage,
  validateConfig,
  validateSemantics,
  validateStructure,
} from '../src/index.ts';
import type { GameAction } from '../src/index.ts';

const FIXTURES = join(dirname(fileURLToPath(import.meta.url)), '..', 'fixtures');

async function sha256Hex(input: string): Promise<string> {
  const bytes = new TextEncoder().encode(input);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

function load(rel: string): unknown {
  return JSON.parse(readFileSync(join(FIXTURES, rel), 'utf8'));
}

function fixtureFiles(dir: string): string[] {
  const full = join(FIXTURES, dir);
  return readdirSync(full)
    .filter((f) => f.endsWith('.json'))
    .map((f) => join(dir, f));
}

describe('protocol versioning', () => {
  it('stamps the current schema version', () => {
    expect(SCHEMA_VERSION).toBe(1);
    expect(PROTOCOL_MAJOR_VERSION).toBe(1);
  });

  it('exposes a stable set of action, event, and error types', () => {
    expect([...ACTION_TYPES].sort()).toEqual([
      'buy_property',
      'decline_buy',
      'end_turn',
      'game_start',
      'player_join',
      'player_leave',
      'player_ready',
      'roll_dice',
    ]);
    expect([...EVENT_TYPES]).toContain('game_started');
    expect([...EVENT_TYPES]).toContain('dice_rolled');
    expect([...EVENT_TYPES]).toContain('turn_advanced');
    expect(ERROR_CODES).toContain('malformed_message');
  });
});

describe('action fixtures', () => {
  for (const file of fixtureFiles('actions')) {
    it(`parses ${file}`, () => {
      const raw = load(file);
      expect(isGameAction(raw), `${file} should be a valid GameAction`).toBe(true);
    });
  }

  it('rejects malformed actions', () => {
    expect(isGameAction({ type: 'player_join' })).toBe(false);
    expect(isGameAction({ type: 'player_ready' })).toBe(false);
    expect(isGameAction({ type: 'teleport' })).toBe(false);
    expect(isGameAction(null)).toBe(false);
  });
});

describe('event fixtures', () => {
  for (const file of fixtureFiles('events')) {
    it(`parses ${file}`, () => {
      const raw = load(file);
      expect(isGameEvent(raw), `${file} should be a valid GameEvent`).toBe(true);
    });
  }
});

describe('message fixtures', () => {
  it.each([
    ['messages/client_hello.json', isHelloMessage],
    ['messages/client_hello_match.json', isHelloMessage],
    ['messages/client_ping.json', isPing],
    ['messages/client_action_join.json', isActionMsg],
    ['messages/server_welcome.json', isWelcomeMessage],
    ['messages/server_pong.json', isPongMessage],
    ['messages/server_event_player_joined.json', isEventMessage],
    ['messages/server_error.json', isErrorMessage],
    ['messages/server_snapshot.json', isSnapshotMessage],
    ['messages/server_catchup.json', isCatchupMessage],
  ])('$1 satisfies its guard', (file, guard) => {
    const raw = load(file);
    expect(guard(raw)).toBe(true);
    expect(isClientMessage(raw) || isServerMessage(raw)).toBe(true);
  });

  function isPing(v: unknown) {
    return isClientMessage(v) && v.type === 'ping';
  }
  function isActionMsg(v: unknown) {
    return isActionMessage(v);
  }

  it('round-trips an action message through JSON without loss', () => {
    const raw = load('messages/client_action_join.json') as GameAction;
    const encoded = JSON.parse(JSON.stringify(raw));
    expect(encoded).toEqual(raw);
  });

  it('validates the snapshot and catchup fixtures structurally', () => {
    const snapshot = load('messages/server_snapshot.json') as {
      snapshot: unknown;
      config: unknown;
    };
    expect(isGameSnapshot(snapshot.snapshot)).toBe(true);
    expect(validateConfig(snapshot.config)).toEqual({ ok: true });
  });
});

describe('config validation tiers', () => {
  it('accepts the default config', () => {
    expect(validateConfig(DEFAULT_CONFIG)).toEqual({ ok: true });
  });

  it('accepts the valid_full fixture', () => {
    expect(validateConfig(load('config/valid_full.json'))).toEqual({ ok: true });
  });

  it('classifies structural issues in invalid_structural.json', () => {
    const result = validateConfig(load('config/invalid_structural.json'));
    if (result.ok) throw new Error('expected structural failure');
    expect(result.issues.length).toBeGreaterThan(0);
    for (const issue of result.issues) {
      expect(issue.kind).toBe('structural');
    }
    const paths = new Set(result.issues.map((i) => i.path));
    expect(paths).toContain('version');
    expect(paths).toContain('startingMoney');
  });

  it('classifies semantic issues in invalid_semantic.json', () => {
    const raw = load('config/invalid_semantic.json');
    const structural = validateStructure(raw);
    expect(structural.ok).toBe(true);

    const result = validateConfig(raw);
    if (result.ok) throw new Error('expected semantic failure');
    for (const issue of result.issues) {
      expect(issue.kind).toBe('semantic');
    }
    const paths = new Set(result.issues.map((i) => i.path));
    expect(paths).toContain('playerCount.min');
    expect(paths).toContain('gambling');
    expect(paths).toContain('randomEvents.intervalTicks');
    expect(paths).toContain('victory.roundLimit');
  });

  it('rejects non-object configs structurally', () => {
    const result = validateConfig([1, 2, 3]);
    expect(result.ok).toBe(false);
  });
});

describe('canonical serialization contract', () => {
  it('matches the shared canonical hash case byte-for-byte', async () => {
    const testCase = load('config/canonical_hash_case.json') as {
      input: unknown;
      canonical: string;
      sha256: string;
    };
    const canonical = canonicalJson(testCase.input);
    expect(canonical).toBe(testCase.canonical);
    const digest = await sha256Hex(canonical);
    expect(digest).toBe(testCase.sha256);
  });
});

describe('validation helper composition', () => {
  it('reports semantic issues only after structure passes', () => {
    const semanticallyBroken = { ...DEFAULT_CONFIG, playerCount: { min: 9, max: 3 } };
    expect(validateStructure(semanticallyBroken).ok).toBe(true);
    expect(validateSemantics(semanticallyBroken).ok).toBe(false);
  });
});
