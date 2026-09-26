# Game Design — Current State

> Status: **v0.2.0 Board Loop**. This document separates what is **decided
> and implemented** (modeled in code and tested), what is **directional**
> (design intent), and what is **undecided**. The website's copy is
> constrained to match this file. Board-level rule decisions are recorded
> in [V0_2_DECISIONS.md](./V0_2_DECISIONS.md).

## Decided and implemented (v0.2.0)

### Match lifecycle

```
Lobby (join · ready up) → Playing → Ended
                    └─(server restart)→ Interrupted
```

- Host starts; all players must be ready; count must be in range.
- Leaving during play eliminates the leaver; if the leaver held the turn,
  it advances deterministically to the next active seat.
- Fewer than two active players ends the match. A sole survivor wins by
  `last_standing`; an empty table ends with `insufficient_players`.
- Seats are assigned in join order and never change.
- **Interrupted** matches (previous process died mid-match) are marked at
  boot and never silently resumed. v0.2 has no live-match restore across
  restarts; disconnect recovery is a separate, supported guarantee.

### Board and movement

- 24 spaces, clockwise (increasing index), space 0 = Start.
- The board is **configuration data** (`GameConfig.board.spaces`), validated
  in three tiers and covered by the canonical config hash. The engine has
  no hardcoded board.
- 16 properties in 4 groups, 3 tax spaces, 4 neutral spaces, 1 start.
- Movement: `(position + d1 + d2) mod len(spaces)`; crossing the start pays
  `propertyRules.passingGoBonus` (default 200) once per crossing. Landing
  exactly on start pays the same single bonus (`land_go`); passing and
  landing never stack.

### Turn state machine

Turn state lives inside `PhasePlaying`; the match phase graph is unchanged.

```
await_roll ──roll──▶ landing resolution
                         ├─ unowned property, affordable ─▶ await_buy_decision
                         ├─ owned by another ─▶ pay rent ─▶ turn_over
                         ├─ tax ─▶ pay bank ─▶ turn_over
                         └─ start / neutral ─▶ turn_over
await_buy_decision ──buy | decline──▶ turn_over (or await_roll on doubles)
turn_over ──end_turn──▶ next active seat, round+1 on wrap
```

- Doubles grant one extra roll; the third consecutive doubles resolves
  normally and then passes the turn (`propertyRules.maxDoublesStreak`).
- A no-op or illegal action never mutates state and never advances the tick.
- Every roll consumes exactly two RNG draws, in fixed order, regardless of
  outcome — uniform stream consumption is a determinism requirement.

### Economy and insolvency

- Chips are integer-only. **No debt:** balances are never negative.
- A mandatory payment a player cannot cover is an **atomic bankruptcy**:
  holdings revert to the bank unowned, the player is eliminated through
  the lifecycle path, the turn auto-advances, and the match end is
  evaluated. No partial payments, mortgages, or negotiation.
- The bank is an explicit counterparty: every inflow/outflow emits a
  `bank_transfer` (`pass_go`, `land_go`, `tax`), so chip conservation is
  provable by folding the event log (tested).
- Rent is paid only to an **active** owner; holdings always revert on
  elimination, so "owned ⟹ owner active" is an engine invariant.
- **Net worth = money + Σ purchase prices of holdings.** There is no resale
  market in v0.2, so purchase price is the single valuation rule.

### Victory conditions

| Condition       | Evaluated                     | Winner                             |
| --------------- | ----------------------------- | ---------------------------------- |
| `last_standing` | after every transition        | last active player                 |
| `target_wealth` | after every transition        | highest net worth (ties: low seat) |
| `round_limit`   | round increments on seat wrap | highest net worth (ties: low seat) |

Priority when several trigger on one transition: last_standing >
target_wealth > round_limit.

### Server-authoritative play

- The client proposes actions; the engine decides. The client renders from
  server snapshots and never computes dice, rent, ownership, or victory.
- Actors come from a session↔token binding. Reconnect tokens are stored
  only as sha256, returned once, and never logged.
- Dispatch order: validate session → validate envelope → resolve actor →
  validate sequence → serialize → apply → persist → publish → ack.
- Delivery is at-least-once; the engine tick is the client-side cursor and
  dedup key. A repeated last `seq` replays its ack; older seqs are stale.
- Recovery: a fresh authoritative snapshot is always sent; missed events
  are replayed only when the cursor is inside the retained window
  (1024 events per match), otherwise the client is told to resync.

### Configuration surface

| Field group                                              | Values                                                  |
| -------------------------------------------------------- | ------------------------------------------------------- |
| `playerCount`                                            | min 2 … max 12                                          |
| `startingMoney`                                          | integer chips > 0                                       |
| `board.spaces`                                           | 2 … 64 spaces: id, kind, name, group, price, rent, tax  |
| `propertyRules`                                          | passing-go bonus, doubles rule, doubles cap             |
| `trading` / `auctions` / `carnival` / `sports` / `cards` | enabled toggles (not yet implemented)                   |
| `gambling`                                               | master switch + poker / blackjack / casino sub-toggles  |
| `randomEvents`                                           | enabled + interval ticks                                |
| `victory`                                                | `last_standing` · `target_wealth(n)` · `round_limit(n)` |

Semantic rules enforced: gambling sub-games require the gambling master
switch; random events require a positive interval; victory params must be
present for their type; a wealth target must exceed starting money; the
first space must be `go`; space ids are unique; property groups, prices,
rents, and tax amounts are in range; min ≤ max players.

## Directional (designed-for, not built)

These have architectural seams but no gameplay:

- **Trading** — offers/counteroffers with escrowed swaps; negotiation UI
  in the application layer.
- **Auctions** — contested assets under the hammer; open and sealed-bid
  formats. Declining a purchase is a terminal no-op, **not** an auction.
- **Gambling tables** — poker nights, blackjack pits, casino floor of quick
  luck games. Play-money only, permanently. Provably-fair RNG disclosure
  is an explicit later workstream.
- **Carnival & sports** — skill-flavored minigames with table-set stakes.
- **Treasure cards** — seeded decks of fortune/misfortune effects.
- **Random & global events** — scheduled table-wide chaos on a configured
  interval.
- **Property development** — `RuntimeSpace.Level` and the rent multiplier
  are reserved; levels stay 0 in v0.2.

## Known limitations (v0.2.0)

- No live-match restore across server restarts; matches are marked
  `interrupted`. Disconnect + reconnect _within_ one process is supported.
- No public matchmaking or lobby listing; matches are private and created
  by id.
- Tokens are per-match and not rate-limited by an account system (none
  exists yet); the per-session action throttle (20 / 10s) is the only
  abuse control.
- Property development, resale, and mortgages are not implemented.

## Undecided (open questions)

Do not treat anything below as settled:

- Event deck authorship format (data-driven packs vs code).
- Ranked/casual separation and matchmaking rating — post-MVP at best.
- Anti-cheat posture beyond authoritative simulation.
- Spectating rules for eliminated players.
- Whether development levels ever affect rent (field exists, no mechanic).

## Milestone mapping

| Milestone        | Scope                                                                                                                                |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| v0.1             | Foundation: architecture, protocol, design system, website, backend, tooling                                                         |
| v0.2 (this repo) | Board loop: board config, dice/movement, landing, buy/decline, rent, tax, insolvency, victory, authoritative server, playable client |
| v0.3+            | Chaos catalog: trading, auctions, gambling tables, carnival, sports, cards, events                                                   |

The website's feature badges (`apps/web/src/data/systems.ts`) are kept in
sync with this file by review convention: if it isn't true here, it can't be
claimed there.
