# Game Design — Current State

> Status: v0.1.0 Foundation. This document separates what is **decided**
> (modeled in code), what is **directional** (design intent), and what is
> **undecided**. The website's copy is constrained to match this file.

## Decided (exists in the repository today)

### Match lifecycle

```
Lobby (join · ready up) → Playing → Ended
```

- Host starts the match; all players must be ready; player count must be
  within the configured range.
- Leaving during play eliminates you; fewer than two active players ends
  the match (`insufficient_players`).
- Seats are assigned in join order and never change.

### Configuration surface

`GameConfig` models these systems per match (validated, versioned,
hashed):

| Field group                                              | Values                                                  |
| -------------------------------------------------------- | ------------------------------------------------------- |
| `playerCount`                                            | min 2 … max 12                                          |
| `startingMoney`                                          | integer chips > 0                                       |
| `trading` / `auctions` / `carnival` / `sports` / `cards` | enabled toggles                                         |
| `gambling`                                               | master switch + poker / blackjack / casino sub-toggles  |
| `randomEvents`                                           | enabled + interval ticks                                |
| `victory`                                                | `last_standing` · `target_wealth(n)` · `round_limit(n)` |

Semantic rules already enforced: gambling sub-games require the gambling
master switch; random events require a positive interval; victory params
must be present for their type; min ≤ max players.

### Economy ground rules

- Money is **integer-only** ("chips"). No fractions, ever.
- Every chip is accounted for: transfers appear as events; nothing
  appears or vanishes outside defined rules.
- Determinism: seeded ChaCha8 streams; subsystem streams are derived via
  labeled splits so they never interfere.

## Directional (designed-for, not built)

These have architectural seams but no gameplay:

- **Property & economy loop** — acquisition, development, rent pressure.
  Payout curves will live in config, not code.
- **Trading** — offers/counteroffers with escrowed swaps; negotiation UI
  in the application layer.
- **Auctions** — contested assets under the hammer; open and sealed-bid
  formats under consideration.
- **Gambling tables** — poker nights, blackjack pits, casino floor of
  quick luck games. Play-money only, permanently. Provably-fair RNG
  disclosure is an explicit later workstream.
- **Carnival & sports** — skill-flavored minigames with stakes set by the
  table.
- **Treasure cards** — seeded decks of fortune/misfortune effects.
- **Random & global events** — scheduled table-wide chaos on a configured
  interval.

## Undecided (open questions)

Do not treat anything below as settled:

- Turn structure granularity (simultaneous phases vs strict rotation).
- Whether eliminated players may spectate or leave silently.
- Event deck authorship format (data-driven packs vs code).
- Ranked/casual separation, matchmaking rating — post-MVP concerns at best.
- Anti-cheat posture beyond authoritative simulation (client attestation etc.).

## Milestone mapping

| Milestone        | Scope                                                                        |
| ---------------- | ---------------------------------------------------------------------------- |
| v0.1 (this repo) | Foundation: architecture, protocol, design system, website, backend, tooling |
| v0.2             | First table: board loop — movement, property, rent, bankruptcy               |
| v0.3+            | Chaos catalog: trading, auctions, gambling tables, carnival, sports, events  |

The website's feature badges (`apps/web/src/data/systems.ts`) are kept in
sync with this file by review convention: if it isn't true here, it can't
be claimed there.
