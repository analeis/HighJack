# v0.2.0 Gameplay Decisions

> Version 1 · 2026-09-25 · status: **approved for implementation**.
> Resolves every open question in `V0_2_BOARD_LOOP_PLAN.md` §13 and
> directive §2 before dependent systems are built. Proposals from the plan
> become rules below only where explicitly stated; everything else stays
> deferred.

## Standing inputs (not re-decided here)

- **v0.1.0 invariants**: deterministic pure engine, versioned TS/Go
  protocol, 3-tier validated + hashed config, SolidJS UI / isolated
  PixiJS, Go modular monolith + PostgreSQL, mandatory certification.
- **Confirmed requirements** (`GAME_DESIGN.md`): integer-only chips, no
  spontaneous money, seeded ChaCha8 streams with labeled splits, lobby →
  playing → ended lifecycle, host start, leave-during-play eliminates.

## 2.1 Board definition

- 24-space clockwise loop (increasing index), defined entirely in
  `GameConfig.board.spaces`. Space 0 is `go`; all players start on it.
- Spaces (id · kind · group · price · rent/tax):
  `go`; `a1`–`a4` Copper $100/10, $120/12, $140/14, $160/16;
  `n1` neutral; `t1` tax $75; `b1`–`b4` Lantern $180/18 … $240/24 with
  `n2`, `t2` $100 interleaved as `go,a1,a2,n1` pattern per §2.1 table
  below; `c1`–`c4` Harbor $260/26 … $320/32; `d1`–`d4` Summit
  $340/34 … $400/40; taxes `t1` $75, `t2` $100, `t3` $150; neutrals
  `n1`–`n4`.
- Exact order: `go,a1,a2,n1,t1,a3,a4,n2,b1,b2,t2,b3,b4,n3,c1,c2,t3,c3,c4,n4,d1,d2,d3,d4`.
- Movement: `(pos + d1 + d2) mod 24`; each wrap past space 23→0 awards
  `passingGoBonus` ($200 default) exactly once per crossing (max one per
  roll: 2d6 ≤ 12 < 24). Landing exactly on `go` collects the same single
  bonus; passing and landing never stack.
- Property groups are config data with **no mechanical effect** in v0.2
  (reserved for development/upgrades later).

## 2.2 Turn and dice rules

- Strict rotation in seat order; no simultaneous phases.
- Doubles grant one extra roll. Three consecutive doubles: the third roll
  still moves and resolves normally (uniform RNG consumption: every roll
  is exactly two `IntN(6)` draws), then the turn passes with no further
  extra roll and the streak resets.
- No extra roll is ever granted by purchasing. Extra-roll eligibility is
  fixed at roll time (`AwardExtraRoll`) and survives the buy decision.
- Turn phases: `await_roll → await_buy_decision → turn_over`, plus the
  doubles return to `await_roll`. A turn is complete only after an
  accepted `end_turn` in `turn_over`.
- Legal-action matrix: `roll` (current seat, active, `await_roll`);
  `buy`/`decline` (current seat, active, `await_buy_decision`);
  `end_turn` (current seat, active, `turn_over`). Everything else is
  rejected; rejection never mutates state or consumes RNG.

## 2.3 Economy and insolvency

- Starting money $1500 (default config); bank is infinite but every
  bank flow is an explicit `bank_transfer` event (reason `pass_go`,
  `land_go`, or `tax`), so chip conservation is auditable.
- Tax transfers the fixed space amount to the bank. Rent = the space's
  configured `rent` (formula `baseRent × (level+1)` reserved; level is
  always 0 in v0.2).
- Rent is paid only to an **active** owner. Holdings always revert to the
  bank on elimination, so owner-active-or-unowned is an engine invariant
  (tested); a non-active owner is treated as unowned (no rent).
- **No debt**: `Money >= 0` always. Any mandatory payment the payer
  cannot cover bankrupts them atomically instead: holdings revert to the
  bank unowned, the player is eliminated via the existing lifecycle path,
  and the turn auto-advances past them (or the match ends).
- Net worth = money + Σ purchase prices of owned spaces. Purchase price
  (not resale — there is no resale in v0.2) is the single valuation rule.

## 2.4 Victory conditions

Existing config semantics are preserved; only their inputs change:

- `last_standing`: one active player remains → they win (the v0.1
  `insufficient_players` end now records the survivor as winner).
- `target_wealth`: checked after every accepted board action; any active
  player with net worth ≥ target ends the match immediately; highest net
  worth wins, ties broken by lowest seat.
- `round_limit`: rounds start at 1 and increment when a turn passes to a
  lower seat number (wrap). When the round exceeds the limit, the match
  ends immediately; richest net worth wins, ties by lowest seat.
- Evaluation priority on a tick that triggers several: last_standing >
  target_wealth > round_limit.

## 2.5 Reconnection and sessions

- Identity: `PlayerID` minted by the server. A private match is an
  unguessable id (`m_` + 16 hex chars); joining mints a 128-bit token
  returned once over HTTP and stored as sha256 hex. No match listing.
- Transport: one WS connection serves one match. `hello` gains optional
  `matchId`, `token`, `resumeFromTick`. The server always answers with a
  full authoritative snapshot; missed events are appended only when the
  supplied cursor is within the retained window, otherwise snapshot-only
  with an explicit resync flag. Client snapshots are never trusted.
- Retention: per-match in-memory ring (cap 1024) plus persisted
  `match_events` rows for active matches. Cursor older than the retained
  base → snapshot, never fabricated catch-up.
- Idempotency: per-player last-applied `seq` watermark; the snapshot
  advertises the next expected `seq`. Repeating the last `seq` replays
  the cached ack; older seqs are rejected as stale.
- Restart: on boot, non-ended matches are marked `interrupted`
  explicitly. No silent partial restore; disconnect recovery and process
  recovery are documented as different guarantees.
- Abuse surface: per-session action rate limit (20 per 10s sliding
  window → `rate_limited`); explicit 64 KiB WS read limit (oversize
  closes the connection); per-match mutex with IO outside the lock;
  slow consumers are dropped, never blocking the match loop.

## Deferred (not v0.2)

Trading, auctions (decline is a terminal no-op), gambling/carnival/
sports/cards/events systems, development levels and group bonuses,
mortgages/debt, spectating, matchmaking, crash-resilient live matches,
client-side prediction.

## v0.2 website/status mapping

`Property & Economy` gains status `live` (violet tone): the board loop is
playable, everything else stays as labeled. The game page drops the
"shell only" framing in favor of the real loop; deferred systems keep
their honest badges.
