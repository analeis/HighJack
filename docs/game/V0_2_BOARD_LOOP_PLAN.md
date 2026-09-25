# v0.2 Board Loop — Design & Implementation Plan

> Status: **plan, not implementation**. Nothing below is built. Mechanics
> marked _(proposal)_ are defaults for review, not decisions; architecture
> marked _(invariant)_ carries v0.1.0 forward unchanged.

## 1. Goal

One complete, server-authoritative, playable board-game loop through the
existing client and server: roll → move → resolve → buy/rent → bankruptcy →
victory. This milestone validates the v0.1.0 architecture under real
gameplay before any chaos-catalog systems (trading, auctions, gambling,
carnival, sports, events) are built.

## 2. Invariants carried forward from v0.1.0

| Boundary    | Invariant (non-negotiable unless implementation evidence shows a defect) |
| ----------- | ------------------------------------------------------------------------ |
| Game engine | Pure deterministic transitions; **no** transport/persistence deps        |
| Protocol    | Explicitly versioned, shared TS/Go contract; additive changes only       |
| Config      | Validated (3 tiers), canonicalized, sha256-hashable                      |
| Frontends   | SolidJS application UI; PixiJS rendering isolated from shared UI         |
| Backend     | Go modular monolith; PostgreSQL for durable state                        |
| Quality     | Certification gates mandatory for every release (`bun run certify`)      |

Extension, not redesign. Any plan item that requires touching
`Engine.Apply`, the envelope format, or the phase graph must justify why
the existing seam is insufficient.

## 3. Non-goals (explicitly deferred to v0.3+)

Trading, auctions (including auction-on-decline), poker/blackjack/casino,
carnival, sports, treasure cards, random/global events, matchmaking,
spectating, chat, replays-as-a-feature. Landing on an unowned property the
actor declines becomes a no-op in v0.2 (documented, not silently dropped —
see §6).

## 4. Engine changes (`server/internal/game`)

### 4.1 State additions

Turn progression lives **inside** `PhasePlaying`, not as new phases — the
`allowedTransitions` graph is untouched:

```go
// Proposed additions to GameState (value fields, never pointers-to-mutable):
Board BoardState `json:"board"` // spaces, ownership, development levels
Turn  TurnState  `json:"turn"`  // whose turn, turn-phase, consecutive doubles
```

- `BoardState`: ordered `Spaces []Space` (fixed at match start from
  config), each space `{ID, Kind, Price, BaseRent, Owner *PlayerID, Level}`.
  _(Proposal: space kinds `go | property | tax | neutral` only.)_
- `TurnState`: `{CurrentSeat Seat, Phase TurnPhase, DoublesStreak int}`
  with `TurnPhase ∈ {await_roll, await_buy_decision, turn_over}`.
  `Player.Position` (space index) is added to `Player`.
- `Clone()` must deep-copy the new slices (existing pattern extends).

### 4.2 New actions (same `Action` interface, new payload structs)

| Action              | Legal when                                                                                   |
| ------------------- | -------------------------------------------------------------------------------------------- |
| `RollDiceAction{}`  | `PhasePlaying`, actor is current seat, `await_roll`                                          |
| `BuyPropertyAction` | `PhasePlaying`, actor is current seat, `await_buy_decision`, space unowned, actor can afford |
| `DeclineBuyAction`  | same gating as buy; explicit no-op that ends the decision (auditable; future auction hook)   |
| `EndTurnAction`     | `PhasePlaying`, actor is current seat, `turn_over` (or decision resolved)                    |

All other conditions → existing domain errors (`ErrOutOfPhase`,
`ErrNotPermitted`); new errors only if no existing one fits.

### 4.3 New events (extend `stamp()` — it panics on unknown types by design)

`dice_rolled {playerId, die1, die2, from, to}`,
`property_bought {playerId, spaceId, price}`,
`buy_declined {playerId, spaceId}`,
`rent_paid {from, to, spaceId, amount}`,
`player_bankrupt {playerId, cause}` (reuses elimination path),
`turn_advanced {seat}`.

### 4.4 Dice & RNG discipline

- Dice consume the tick stream directly: `d1 = rng.IntN(6)+1`,
  `d2 = rng.IntN(6)+1`, in that fixed order. Same tick ⇒ same roll,
  covered by the existing determinism test shape.
- _(Proposal)_ doubles grant one extra roll (`DoublesStreak`, capped at 3
  → turn passes). Any streak rule must be a pure function of the roll
  history, never of wall-clock or connection state.

### 4.5 Turn-state machine (per-action transitions)

```
await_roll → (roll) → move → resolve landing:
  ├─ unowned property + affordable → await_buy_decision
  ├─ owned by other → pay rent (event) → turn_over
  ├─ go/tax/neutral → apply effect (event) → turn_over
  └─ unaffordable/unowned → turn_over
await_buy_decision → (buy | decline) → turn_over
turn_over → (end_turn) → advance seat → await_roll
```

Rent that the payer cannot afford triggers insolvency (§6), never a
negative balance: `Money` stays `>= 0` by construction.

## 5. Board & property data model _(proposals for review)_

- **Starting board**: one fixed loop of 24 spaces defined as config data
  (not code): 1 go, 16 properties in 4 color groups, 3 tax, 4 neutral.
  JSON schema for spaces lives in config; unknown space kinds are
  structural errors.
- **Property economics**: `price` and `baseRent` per space from board
  data; rent _(proposal)_ = `baseRent × (level + 1)`, level 0 only in v0.2
  (development arrives later — the `Level` field reserves it).
- **Payout curves live in config, not code** (per GAME_DESIGN.md
  direction): `property_rules {passingGoBonus, bankruptcyRule}`.

## 6. Economy rules _(proposals for review)_

- **Buy**: `money -= price`, ownership set, `property_bought` emitted.
  Atomic with validation; unaffordable → `ErrNotPermitted`-family error,
  state untouched.
- **Rent**: `payer -= rent; owner += rent; rent_paid` emitted. The bank is
  an explicit counterparty for go-bonus/tax so chip conservation is
  auditable: every chip movement appears in exactly one event.
- **Insolvency** _(proposal)_: any payment that would drive money below 0
  bankrupts the payer instead — holdings revert to the bank (unowned),
  `player_bankrupt` + elimination events emitted, existing
  `maybeEndForInsufficientPlayers` decides the match end. No debt, no
  partial payment in v0.2.
- **Victory**: reuse configured conditions against **net worth**
  _(proposal: money + sum of purchase prices of holdings)_ —
  `last_standing` unchanged; `target_wealth`/`round_limit` compare net
  worth. Keeps v0.1 config semantics intact.

## 7. Configuration extensions

`GameConfig` gains `board {spaces[]}` and `property_rules {}` sections,
mirrored in `packages/protocol/src/config.ts` with the same three
validation tiers (unknown space kind → structural; e.g. `min > max`
style impossibilities → semantic). Canonical JSON hashing covers the new
fields with no code change. New fixtures:
`valid_board.json`, `invalid_board_structural.json`. `SCHEMA_VERSION`
bump only if wire shape breaks; additive fields keep version 1.

## 8. Protocol additions

Envelopes unchanged. Additive vocabulary only → **minor** version bump
(`1.1.0`) per `docs/protocol/OVERVIEW.md` rules; `v: 1` major stamp
unchanged so old clients are rejected cleanly, not misparsed. TS
`GameAction`/`GameEvent` unions + Go decode paths extend in lockstep,
pinned by shared fixtures (new `fixtures/actions/*`,
`fixtures/events/*`).

## 9. Server work (biggest v0.2 item)

Fill the realtime seam without redesigning it:

- **Match registry**: `lobby` (or new `match` package — decide at
  implementation; boundary matters more than the name) holding
  `Engine + GameState + config` per match id, guarded by mutex.
- **Session → actor binding**: handshake already yields a session;
  `player_join` binds it to a minted `PlayerID`. Actions derive the actor
  from the connection — never from the payload (existing invariant).
- **Dispatch**: decode → `Engine.Apply` → broadcast resulting events to
  match connections; `not_supported` shrinks to truly unknown types.
- **Persistence**: `games`/`game_players` rows now track live money,
  position, and status; board ownership snapshot per match (new table or
  `JSONB` column — decide at implementation, migrate explicitly).
  **Reconnection** _(proposal)_: rejoin with same session replays state
  snapshot + missed events since a client-supplied tick; no speculative
  client simulation — server snapshot is truth.

## 10. Client work (`apps/game`)

- New `src/net/` boundary: WS connect, hello/welcome, action send with
  `seq`, event subscription into `shell-store` (extended, not replaced).
- PixiJS renders the board ring from state snapshots (positions, owners);
  SolidJS owns the action dock — Roll/Buy/Decline/End Turn enabled **only**
  when the server state marks them legal for the local seat.
- The action dock's disabled buttons become live; the "concept preview"
  badge is replaced by a connection/match status line. No gameplay is
  simulated client-side, ever.

## 11. Test plan (gates stay mandatory)

- **Engine** (table-driven, `input + seed ⇒ state/events`): full-lap walk,
  buy happy path + unaffordable rejection (state untouched), rent transfer
  exactness, bankruptcy cascade (rent → bankrupt → eliminate →
  `insufficient_players` → ended), doubles streak incl. cap, decline
  no-op emits exactly one event, determinism byte-equality rerun.
- **Invariants** (new, enforced): chip conservation across every event
  sequence (bank counterparty makes this checkable); ownership uniqueness;
  `Money >= 0` always; one tick per action; no-op ⇒ no events.
- **Protocol compat**: new fixtures decoded on both sides; canonical-hash
  equality incl. board sections.
- **Multiplayer integration** (Go, real WS dial over loopback): two
  clients join/ready/start/roll/buy; disconnect mid-turn → elimination
  path; reconnect → snapshot + catch-up.
- **Browser**: game-shell route drives a scripted match (or documents why
  not, if flakiness demands — no flaky mandatory gates per
  CERTIFICATION.md).
- **Certification**: `bun run certify` green; no new infrastructure.

## 12. Implementation order

1. Engine: state fields → actions/events (+`stamp`) → turn machine →
   dice → buy/rent/insolvency → tests green.
2. Config + protocol mirrors + fixtures (both sides, lockstep).
3. Server: registry → dispatch/broadcast → persistence + reconnect.
4. Client: net boundary → board render → action gating.
5. Integration tests → docs sync (GAME_DESIGN.md decided/directional
   lines move) → certify.

## 13. Open questions (decide before/with 4.1, not during 9–10)

1. Exact 24-space board contents and numbers (proposal in §5 is a
   starting bid).
2. Doubles rule: extra roll vs nothing in v0.2.
3. Net-worth definition for wealth victories.
4. Insolvency strictness: instant elimination (proposal) vs
   mortgage/negotiation hooks (deferred complexity — recommend defer).
5. Reconnect window and missed-event retention bounds.
