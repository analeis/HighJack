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

export const SPACE_KINDS = ['go', 'property', 'tax', 'neutral'] as const;
export type SpaceKind = (typeof SPACE_KINDS)[number];

export interface BoardSpace {
  readonly id: string;
  readonly kind: SpaceKind;
  /** Display label shown on the rendered board. */
  readonly name: string;
  /** Property group (cosmetic in v0.2; reserved for development rules). */
  readonly group: string;
  readonly price: number;
  readonly rent: number;
  /** Fixed tax amount for kind = "tax". */
  readonly amount: number;
}

export interface BoardConfig {
  readonly spaces: readonly BoardSpace[];
}

export interface PropertyRules {
  /** Chips awarded per start crossing. */
  readonly passingGoBonus: number;
  /** Doubles grant an extra roll when true. */
  readonly doublesExtraRoll: boolean;
  /** Consecutive-doubles cap after which the turn passes. */
  readonly maxDoublesStreak: number;
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
  readonly board: BoardConfig;
  readonly propertyRules: PropertyRules;
}

/** The standard 24-space loop. Mirrors DefaultBoard in server/internal/game. */
export const DEFAULT_BOARD: readonly BoardSpace[] = [
  { id: 'go', kind: 'go', name: 'Start', group: '', price: 0, rent: 0, amount: 0 },
  {
    id: 'a1',
    kind: 'property',
    name: 'Copper Row',
    group: 'Copper',
    price: 100,
    rent: 10,
    amount: 0,
  },
  {
    id: 'a2',
    kind: 'property',
    name: 'Tin Lane',
    group: 'Copper',
    price: 120,
    rent: 12,
    amount: 0,
  },
  { id: 'n1', kind: 'neutral', name: 'Old Fountain', group: '', price: 0, rent: 0, amount: 0 },
  { id: 't1', kind: 'tax', name: 'Toll Gate', group: '', price: 0, rent: 0, amount: 75 },
  {
    id: 'a3',
    kind: 'property',
    name: 'Brass Way',
    group: 'Copper',
    price: 140,
    rent: 14,
    amount: 0,
  },
  {
    id: 'a4',
    kind: 'property',
    name: 'Nickel Court',
    group: 'Copper',
    price: 160,
    rent: 16,
    amount: 0,
  },
  { id: 'n2', kind: 'neutral', name: 'Night Market', group: '', price: 0, rent: 0, amount: 0 },
  {
    id: 'b1',
    kind: 'property',
    name: 'Lantern Row',
    group: 'Lantern',
    price: 180,
    rent: 18,
    amount: 0,
  },
  {
    id: 'b2',
    kind: 'property',
    name: 'Wick Street',
    group: 'Lantern',
    price: 200,
    rent: 20,
    amount: 0,
  },
  { id: 't2', kind: 'tax', name: 'Harbor Toll', group: '', price: 0, rent: 0, amount: 100 },
  {
    id: 'b3',
    kind: 'property',
    name: 'Glow Alley',
    group: 'Lantern',
    price: 220,
    rent: 22,
    amount: 0,
  },
  {
    id: 'b4',
    kind: 'property',
    name: 'Beacon Court',
    group: 'Lantern',
    price: 240,
    rent: 24,
    amount: 0,
  },
  { id: 'n3', kind: 'neutral', name: 'Grand Plaza', group: '', price: 0, rent: 0, amount: 0 },
  {
    id: 'c1',
    kind: 'property',
    name: 'Dockside Row',
    group: 'Harbor',
    price: 260,
    rent: 26,
    amount: 0,
  },
  {
    id: 'c2',
    kind: 'property',
    name: 'Anchor Lane',
    group: 'Harbor',
    price: 280,
    rent: 28,
    amount: 0,
  },
  { id: 't3', kind: 'tax', name: 'Crown Tax', group: '', price: 0, rent: 0, amount: 150 },
  { id: 'c3', kind: 'property', name: 'Tideway', group: 'Harbor', price: 300, rent: 30, amount: 0 },
  {
    id: 'c4',
    kind: 'property',
    name: 'Lighthouse Point',
    group: 'Harbor',
    price: 320,
    rent: 32,
    amount: 0,
  },
  { id: 'n4', kind: 'neutral', name: 'Sky Garden', group: '', price: 0, rent: 0, amount: 0 },
  {
    id: 'd1',
    kind: 'property',
    name: 'Summit Rise',
    group: 'Summit',
    price: 340,
    rent: 34,
    amount: 0,
  },
  {
    id: 'd2',
    kind: 'property',
    name: 'Cloud Terrace',
    group: 'Summit',
    price: 360,
    rent: 36,
    amount: 0,
  },
  {
    id: 'd3',
    kind: 'property',
    name: 'Peak View',
    group: 'Summit',
    price: 380,
    rent: 38,
    amount: 0,
  },
  {
    id: 'd4',
    kind: 'property',
    name: 'Crown Heights',
    group: 'Summit',
    price: 400,
    rent: 40,
    amount: 0,
  },
];

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
  board: { spaces: [...DEFAULT_BOARD] },
  propertyRules: { passingGoBonus: 200, doublesExtraRoll: true, maxDoublesStreak: 3 },
};

// ---- limits ----------------------------------------------------------------

export const LIMITS = {
  minPlayersFloor: 2,
  maxPlayersCeiling: 12,
  startingMoneyMax: 1_000_000,
  minBoardSpaces: 2,
  maxBoardSpaces: 64,
  maxSpaceIdLen: 32,
  maxSpaceName: 64,
  maxGroupLen: 32,
  maxSpacePrice: 1_000_000,
  maxSpaceRent: 100_000,
  maxTaxAmount: 1_000_000,
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

  const board = value['board'];
  if (!isRecord(board)) {
    issues.push({ kind: 'structural', path: 'board', message: 'must be an object' });
  } else {
    for (const key of Object.keys(board)) {
      if (key !== 'spaces') {
        issues.push({
          kind: 'structural',
          path: `board.${key}`,
          message: `unknown field ${JSON.stringify(key)}`,
        });
      }
    }
    const spaces = board['spaces'];
    if (!Array.isArray(spaces)) {
      issues.push({ kind: 'structural', path: 'board.spaces', message: 'must be an array' });
    } else {
      spaces.forEach((item, i) => {
        const path = `board.spaces[${i}]`;
        if (!isRecord(item)) {
          issues.push({ kind: 'structural', path, message: 'must be an object' });
          return;
        }
        for (const f of ['id', 'kind', 'name', 'group'] as const) {
          if (typeof item[f] !== 'string') {
            issues.push({ kind: 'structural', path: `${path}.${f}`, message: 'must be a string' });
          }
        }
        for (const f of ['price', 'rent', 'amount'] as const) {
          if (typeof item[f] !== 'number' || !Number.isSafeInteger(item[f])) {
            issues.push({
              kind: 'structural',
              path: `${path}.${f}`,
              message: 'must be an integer',
            });
          }
        }
        for (const key of Object.keys(item)) {
          if (!['id', 'kind', 'name', 'group', 'price', 'rent', 'amount'].includes(key)) {
            issues.push({
              kind: 'structural',
              path: `${path}.${key}`,
              message: `unknown field ${JSON.stringify(key)}`,
            });
          }
        }
      });
    }
  }

  const pr = value['propertyRules'];
  if (!isRecord(pr)) {
    issues.push({ kind: 'structural', path: 'propertyRules', message: 'must be an object' });
  } else {
    const bonus = pr['passingGoBonus'];
    if (typeof bonus !== 'number' || !Number.isSafeInteger(bonus) || bonus < 0) {
      issues.push({
        kind: 'structural',
        path: 'propertyRules.passingGoBonus',
        message: 'must be a non-negative integer',
      });
    }
    if (typeof pr['doublesExtraRoll'] !== 'boolean') {
      issues.push({
        kind: 'structural',
        path: 'propertyRules.doublesExtraRoll',
        message: 'must be a boolean',
      });
    }
    const streak = pr['maxDoublesStreak'];
    if (typeof streak !== 'number' || !Number.isSafeInteger(streak) || streak < 0) {
      issues.push({
        kind: 'structural',
        path: 'propertyRules.maxDoublesStreak',
        message: 'must be a non-negative integer',
      });
    }
    for (const key of Object.keys(pr)) {
      if (!['passingGoBonus', 'doublesExtraRoll', 'maxDoublesStreak'].includes(key)) {
        issues.push({
          kind: 'structural',
          path: `propertyRules.${key}`,
          message: `unknown field ${JSON.stringify(key)}`,
        });
      }
    }
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

  // Value checks assume a sound shape, mirroring the Go pipeline where
  // generic shape issues short-circuit typed validation.
  if (issues.length === 0) {
    validateBoardValues(value as unknown as GameConfig, issues);
  }

  return issues.length === 0 ? { ok: true } : { ok: false, issues };
}

/** Board value validation: dimensions, ordering, kinds, ranges. */
function validateBoardValues(config: GameConfig, issues: ConfigIssue[]): void {
  const push = (path: string, message: string) =>
    issues.push({ kind: 'structural', path, message });
  const spaces = config.board.spaces;
  if (spaces.length < LIMITS.minBoardSpaces || spaces.length > LIMITS.maxBoardSpaces) {
    push(
      'board.spaces',
      `must have between ${LIMITS.minBoardSpaces} and ${LIMITS.maxBoardSpaces} spaces`,
    );
    return;
  }
  if (spaces[0]!.kind !== 'go') {
    push('board.spaces[0].kind', 'first space must be go');
  }
  const seen = new Set<string>();
  spaces.forEach((s, i) => {
    const path = `board.spaces[${i}]`;
    if (s.id.length === 0 || s.id.length > LIMITS.maxSpaceIdLen) {
      push(`${path}.id`, `must be 1..${LIMITS.maxSpaceIdLen} characters`);
    } else if (seen.has(s.id)) {
      push(`${path}.id`, `duplicate space id ${JSON.stringify(s.id)}`);
    } else {
      seen.add(s.id);
    }
    if (s.name.length === 0 || s.name.length > LIMITS.maxSpaceName) {
      push(`${path}.name`, `must be 1..${LIMITS.maxSpaceName} characters`);
    }
    switch (s.kind) {
      case 'go':
      case 'neutral':
        break;
      case 'property':
        if (s.group.length === 0 || s.group.length > LIMITS.maxGroupLen) {
          push(`${path}.group`, `property group must be 1..${LIMITS.maxGroupLen} characters`);
        }
        if (!(s.price >= 1 && s.price <= LIMITS.maxSpacePrice)) {
          push(`${path}.price`, `must be between 1 and ${LIMITS.maxSpacePrice}`);
        }
        if (!(s.rent >= 0 && s.rent <= LIMITS.maxSpaceRent)) {
          push(`${path}.rent`, `must be between 0 and ${LIMITS.maxSpaceRent}`);
        }
        break;
      case 'tax':
        if (!(s.amount >= 1 && s.amount <= LIMITS.maxTaxAmount)) {
          push(`${path}.amount`, `must be between 1 and ${LIMITS.maxTaxAmount}`);
        }
        break;
      default:
        push(`${path}.kind`, `unknown space kind ${JSON.stringify(s.kind)}`);
        break;
    }
  });
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
      } else if (config.victory.targetWealth <= config.startingMoney) {
        push('victory.targetWealth', 'target wealth must exceed starting money');
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
  if (config.propertyRules.doublesExtraRoll && config.propertyRules.maxDoublesStreak < 1) {
    push('propertyRules.maxDoublesStreak', 'must be at least 1 when doubles grant extra rolls');
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
