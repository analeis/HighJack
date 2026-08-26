/**
 * GameConfig wire schema.
 *
 * GameConfig is a first-class domain object: a HighJack match is a
 * configurable ruleset, and this schema is its wire representation. The
 * authoritative model and validator live in the Go engine
 * (server/internal/game); this module mirrors that contract so clients can
 * build and pre-validate configurations without a server round-trip.
 *
 * Validation tiers (mirrored on both sides):
 * - syntactic   → JSON parse failure (handled by transport, not here)
 * - structural  → wrong shape/types/ranges        → ConfigIssue(kind="structural")
 * - semantic    → logically impossible combination → ConfigIssue(kind="semantic")
 */
import { SCHEMA_VERSION } from './version.ts';

export const CONFIG_SCHEMA_VERSION = SCHEMA_VERSION;

export interface PlayerCountRange {
  readonly min: number;
  readonly max: number;
}

export interface TradingConfig {
  readonly enabled: boolean;
}

export interface AuctionsConfig {
  readonly enabled: boolean;
}

export interface GamblingConfig {
  /** Master switch for all wager-based subsystems. */
  readonly enabled: boolean;
  readonly poker: boolean;
  readonly blackjack: boolean;
  /** Generic casino floor (wheel, slots-style minigames). */
  readonly casino: boolean;
}

export interface CarnivalConfig {
  readonly enabled: boolean;
}

export interface SportsConfig {
  readonly enabled: boolean;
}

export interface CardsConfig {
  readonly enabled: boolean;
}

export interface RandomEventsConfig {
  readonly enabled: boolean;
  /** Emit a random global event every N ticks. Must be > 0 when enabled. */
  readonly intervalTicks: number;
}

export const VICTORY_TYPES = ['last_standing', 'target_wealth', 'round_limit'] as const;
export type VictoryType = (typeof VICTORY_TYPES)[number];

export interface VictoryConfig {
  readonly type: VictoryType;
  /** Required when type = "target_wealth"; ignored otherwise. */
  readonly targetWealth: number;
  /** Required when type = "round_limit"; ignored otherwise. */
  readonly roundLimit: number;
}

/**
 * The full configuration surface. Fields not yet backed by implemented
 * gameplay are still modeled here so that configuration tooling, storage,
 * and protocol handling are stable while systems come online feature by
 * feature. Unimplemented subsystems simply never affect simulation output.
 */
export interface GameConfig {
  readonly version: number;
  readonly playerCount: PlayerCountRange;
  readonly startingMoney: number;
  readonly trading: TradingConfig;
  readonly auctions: AuctionsConfig;
  readonly gambling: GamblingConfig;
  readonly carnival: CarnivalConfig;
  readonly sports: SportsConfig;
  readonly cards: CardsConfig;
  readonly randomEvents: RandomEventsConfig;
  readonly victory: VictoryConfig;
}

/** The default configuration used by the website preview and dev server. */
export const DEFAULT_CONFIG: GameConfig = {
  version: CONFIG_SCHEMA_VERSION,
  playerCount: { min: 2, max: 8 },
  startingMoney: 1500,
  trading: { enabled: true },
  auctions: { enabled: true },
  gambling: { enabled: false, poker: false, blackjack: false, casino: false },
  carnival: { enabled: false },
  sports: { enabled: false },
  cards: { enabled: true },
  randomEvents: { enabled: false, intervalTicks: 0 },
  victory: { type: 'last_standing', targetWealth: 0, roundLimit: 0 },
};

// ---- limits ----------------------------------------------------------------

export const LIMITS = {
  minPlayersFloor: 2,
  maxPlayersCeiling: 12,
  startingMoneyMax: 1_000_000,
} as const;

// ---- validation ------------------------------------------------------------

export type IssueKind = 'structural' | 'semantic';

export interface ConfigIssue {
  readonly kind: IssueKind;
  /** Dotted path into the config object, e.g. "playerCount.min". */
  readonly path: string;
  readonly message: string;
}

export type ValidationResult =
  { readonly ok: true } | { readonly ok: false; readonly issues: readonly ConfigIssue[] };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function bool(
  v: Record<string, unknown>,
  key: string,
  path: string,
  issues: ConfigIssue[],
): boolean {
  const val = v[key];
  if (typeof val !== 'boolean') {
    issues.push({ kind: 'structural', path, message: `must be a boolean` });
    return false;
  }
  return val;
}

function int(
  v: Record<string, unknown>,
  key: string,
  path: string,
  issues: ConfigIssue[],
): number | undefined {
  const val = v[key];
  if (typeof val !== 'number' || !Number.isSafeInteger(val)) {
    issues.push({ kind: 'structural', path, message: 'must be an integer' });
    return undefined;
  }
  return val;
}

/** Structural validation: shape, types, and hard ranges. */
export function validateStructure(value: unknown): ValidationResult {
  const issues: ConfigIssue[] = [];
  if (!isRecord(value)) {
    return {
      ok: false,
      issues: [{ kind: 'structural', path: '', message: 'config must be an object' }],
    };
  }

  if (int(value, 'version', 'version', issues) !== CONFIG_SCHEMA_VERSION) {
    issues.push({
      kind: 'structural',
      path: 'version',
      message: `must be ${CONFIG_SCHEMA_VERSION}`,
    });
  }

  const pc = value['playerCount'];
  if (!isRecord(pc)) {
    issues.push({ kind: 'structural', path: 'playerCount', message: 'must be an object' });
  } else {
    const min = int(pc, 'min', 'playerCount.min', issues);
    const max = int(pc, 'max', 'playerCount.max', issues);
    if (min !== undefined && min < LIMITS.minPlayersFloor) {
      issues.push({
        kind: 'structural',
        path: 'playerCount.min',
        message: `must be ≥ ${LIMITS.minPlayersFloor}`,
      });
    }
    if (max !== undefined && max > LIMITS.maxPlayersCeiling) {
      issues.push({
        kind: 'structural',
        path: 'playerCount.max',
        message: `must be ≤ ${LIMITS.maxPlayersCeiling}`,
      });
    }
  }

  const sm = int(value, 'startingMoney', 'startingMoney', issues);
  if (sm !== undefined && (sm <= 0 || sm > LIMITS.startingMoneyMax)) {
    issues.push({
      kind: 'structural',
      path: 'startingMoney',
      message: `must be between 1 and ${LIMITS.startingMoneyMax}`,
    });
  }

  for (const [key, sub] of [
    ['trading', ['enabled']],
    ['auctions', ['enabled']],
    ['carnival', ['enabled']],
    ['sports', ['enabled']],
    ['cards', ['enabled']],
  ] as const) {
    const obj = value[key];
    if (!isRecord(obj)) {
      issues.push({ kind: 'structural', path: key, message: 'must be an object' });
    } else {
      bool(obj, sub[0]!, key + '.enabled', issues);
    }
  }

  const gamb = value['gambling'];
  if (!isRecord(gamb)) {
    issues.push({ kind: 'structural', path: 'gambling', message: 'must be an object' });
  } else {
    bool(gamb, 'enabled', 'gambling.enabled', issues);
    bool(gamb, 'poker', 'gambling.poker', issues);
    bool(gamb, 'blackjack', 'gambling.blackjack', issues);
    bool(gamb, 'casino', 'gambling.casino', issues);
  }

  const re = value['randomEvents'];
  if (!isRecord(re)) {
    issues.push({ kind: 'structural', path: 'randomEvents', message: 'must be an object' });
  } else {
    bool(re, 'enabled', 'randomEvents.enabled', issues);
    int(re, 'intervalTicks', 'randomEvents.intervalTicks', issues);
  }

  const vic = value['victory'];
  if (!isRecord(vic)) {
    issues.push({ kind: 'structural', path: 'victory', message: 'must be an object' });
  } else {
    const t = vic['type'];
    if (typeof t !== 'string' || !VICTORY_TYPES.includes(t as VictoryType)) {
      issues.push({ kind: 'structural', path: 'victory.type', message: 'unknown victory type' });
    }
    int(vic, 'targetWealth', 'victory.targetWealth', issues);
    int(vic, 'roundLimit', 'victory.roundLimit', issues);
  }

  return issues.length === 0 ? { ok: true } : { ok: false, issues };
}

/** Semantic validation: logically impossible combinations. Assumes structure is valid. */
export function validateSemantics(config: GameConfig): ValidationResult {
  const issues: ConfigIssue[] = [];
  const push = (path: string, message: string) => issues.push({ kind: 'semantic', path, message });

  if (config.playerCount.min > config.playerCount.max) {
    push('playerCount.min', 'min players exceeds max players');
  }
  if (config.gambling.poker || config.gambling.blackjack || config.gambling.casino) {
    if (!config.gambling.enabled) {
      push('gambling', 'individual gambling games require gambling.enabled');
    }
  }
  if (config.randomEvents.enabled && config.randomEvents.intervalTicks <= 0) {
    push('randomEvents.intervalTicks', 'interval must be positive when random events are enabled');
  }
  switch (config.victory.type) {
    case 'target_wealth':
      if (config.victory.targetWealth <= 0) {
        push('victory.targetWealth', 'target wealth must be positive for target_wealth victory');
      }
      break;
    case 'round_limit':
      if (config.victory.roundLimit <= 0) {
        push('victory.roundLimit', 'round limit must be positive for round_limit victory');
      }
      break;
    default:
      break;
  }

  return issues.length === 0 ? { ok: true } : { ok: false, issues };
}

/** Full validation pipeline: structural then semantic. */
export function validateConfig(value: unknown): ValidationResult {
  const structural = validateStructure(value);
  if (!structural.ok) return structural;
  return validateSemantics(value as GameConfig);
}

// ---- canonical serialization ----------------------------------------------

/**
 * Deterministic canonical JSON: objects with sorted keys, no whitespace,
 * arrays in order. Identical implementations exist in TypeScript (here)
 * and Go (server/internal/game) so both compute identical config hashes.
 */
export function canonicalJson(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(',')}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonicalJson(v)}`).join(',')}}`;
}
