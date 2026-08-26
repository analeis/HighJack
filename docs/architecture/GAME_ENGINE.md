# The Game Engine Boundary

> Status: v0.1.0 Foundation — the engine skeleton and lifecycle ruleset are
> real and tested; board gameplay is not implemented yet.

## Location & independence

The engine lives at `server/internal/game` (Go) and depends **only on the
Go standard library**. It knows nothing about HTTP, WebSockets, PostgreSQL,
authentication, JSON transport, or frontends. The server calls it directly
as a library.

## The model

```
Action
  ↓ Validation
State Transition   (exactly one tick)
  ↓ Events
New State
```

### Types

| Concept      | Go                                       | Notes                                                                                                            |
| ------------ | ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `GameState`  | `GameState` struct                       | Plain serializable value: phase, tick, players, end metadata. No wall-clock time inside state.                   |
| `GameConfig` | `GameConfig` struct                      | Validated configuration; canonical-JSON hash pins the exact ruleset of a match.                                  |
| `Player`     | `Player` struct                          | Seat order is fixed; money is integer-only.                                                                      |
| `Action`     | `Action` interface + payloads            | Requests, never effects. Actor identity comes from the server (connection), never from the payload.              |
| `Event`      | `Event` interface + payloads             | Facts about what changed, stamped with the producing tick.                                                       |
| `Ruleset`    | `Ruleset` interface                      | Binds action types to validation+transition logic. Lifecycle today; future board/minigame rulesets plug in here. |
| Phase        | `PhaseLobby → PhasePlaying → PhaseEnded` | Explicit transition table; undocumented transitions do not exist.                                                |

### Engine guarantees

1. **Atomicity** — a failed `Apply` leaves state untouched (tested).
2. **Determinism** — same seed + same action sequence ⇒ identical state
   bytes and event log (tested).
3. **Tick discipline** — exactly one tick per applied action.
4. **Events correspond to transitions** — a no-op action emits nothing.
5. **No hidden randomness** — handlers receive an `Rng`; nothing else in
   domain logic generates numbers.

## Deterministic RNG design

```go
type Rng interface{ Uint64() uint64; IntN(n int) int }
```

- `SeededRng`: ChaCha8-based stream from an explicit 256-bit `Seed`
  (`math/rand/v2`). Platform-independent output.
- `Split(parent, label)`: derives independent child streams by hashing the
  parent seed with a label. Subsystems get their own streams so consuming
  randomness for cards never perturbs dice, etc.
- `DeriveMatchSeed(root, tick)`: reproduces the per-match seed recorded in
  `game_started` events.

This abstraction is the foundation for future seeded simulations, replays,
deterministic tests, and verifiable randomness. It deliberately does not
implement gambling-grade provable fairness yet.

## Configuration validation tiers

`ParseGameConfig` distinguishes three failure classes:

1. **Syntactic** — not valid JSON at all → single structural issue.
2. **Structural** — wrong shape/types/ranges/unknown fields → per-field
   issues with dotted paths (`playerCount.min`, …), reported before any
   semantic judgment.
3. **Semantic** — logically impossible combinations (poker enabled while
   gambling disabled; random events enabled with interval 0; target_wealth
   victory without a positive target) → separate issue kind.

The TypeScript mirror (`packages/protocol/src/config.ts`) implements the
same tiers with the same paths; shared fixtures under
`packages/protocol/fixtures/config/` pin both sides.

## Testing philosophy

Every engine test follows:

```
input state + action + seed ⇒ expected state/events
```

Conventions established in `server/internal/game/*_test.go`:

- Table-driven cases over fixtures where wire shapes matter.
- Determinism tests run the same scenario twice and compare serialized
  state + event logs byte-for-byte.
- Invariant tests: invalid actions cannot mutate state; ticks advance
  exactly once; phase transitions are guarded.

Future suites should add property-based tests (randomized-but-seeded
action sequences checked against invariants) and replay tests (recorded
event logs re-applied must reproduce final state). See
[../development/TESTING.md](../development/TESTING.md).

## What the first real ruleset will look like

The lifecycle ruleset (`lifecycle.go`) demonstrates the full pattern:
per-phase guards, seat/money bookkeeping, event emission, host transfer,
and terminal-state handling. A board-game ruleset adds its own action types
(`roll_dice`, `buy_property`, …) through the same `Ruleset` interface —
no engine-core changes required.
