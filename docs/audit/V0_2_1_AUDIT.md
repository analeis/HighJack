# v0.2.0 Post-Release Audit — Findings Register & Patchwork Plan

> Status: **audit of the released v0.2.0 tree**, complete. This document is
> the findings register for the v0.2.x stabilization cycle. It records what
> was verified, what was reproduced, and what each patch release is
> accountable for.
>
> Authority for shipped behavior remains
> [GAME_DESIGN.md](../game/GAME_DESIGN.md). Where this document and
> BACKEND.md / TESTING.md disagree, **this document is right until the
> corresponding patch lands** — the discrepancies are themselves findings
> (OPS-2, OPS-3) and are corrected in v0.2.1.

---

## 1. Verified baseline and release identity

| Item                        | Value                                                            |
| --------------------------- | ---------------------------------------------------------------- |
| Repository                  | `git@github-aleis:analeis/HighJack.git`                          |
| Branch                      | `main`                                                           |
| Audited commit              | `494ac57c40d9deb28dc73e6b6d2d3b76685dd7d3`                       |
| `v0.2.0` tag                | annotated → `494ac57` (not moved by this cycle)                  |
| `v0.1.0` tag                | annotated → `8f2fec1` (intact)                                   |
| Working tree at audit       | clean, in sync with `origin/main`                                |
| Commits since `v0.1.0`      | 8                                                                |
| Certification report        | `verification/report.json` — **gitignored**, regenerated per run |
| Report commit at audit time | `7ec826b` — **one commit behind the tag** (see OPS-1)            |

Baseline pipeline result, captured **before** any change:

```
go test -count=1 -race ./server/...      → all packages ok (with a real PostgreSQL)
bun run certify                         → exit 0, 14/14 gates, CERTIFIED
```

The tree is green. That is the finding: **green is not evidence here**, because
the gate list does not cover the layers where the defects are (OPS), the DB
tests silently skip while reporting `passed` (OPS-4), and the report does not
identify the tree it actually tested (OPS-1).

### Audit method

Static inspection of every file in `server/internal/{game,protocol,match,realtime,persistence,api,config}`,
`packages/protocol/src`, `apps/game/src`, the migration files, the pipeline
scripts, and CI — plus targeted reproduction of the four certification
defects. Every P0/P1 claim below was independently re-read at its cited
`file:line` before being recorded. Findings that could not be traced to
certainty are separated into §8.

---

## 2. Executive summary

**The engine is sound. The layers above it are not.**

An independent pass over `server/internal/game` confirmed twenty properties
safe, including: uniform RNG draw counts (exactly 2 for `roll_dice`, 0 for
every other action, order independent of outcome), `Clone` completeness for
all slice fields, no state mutation on any rejected action, exact-once tick
stamping, no reachable stuck `PhasePlaying` state, and correct pass-go /
land-go single-payment accounting. The authorization boundary is also sound:
**no impersonation path and no cross-match action path exists**, and this was
attempted rather than assumed.

The defects concentrate above the engine, and the common cause is narrow and
specific: **`server/internal/match/` — 511 lines holding the entire
authorization, idempotency, durability-ordering, and broadcast layer — has no
test file.** Nothing in the repository has ever placed two clients in one
match. `multiplayer_test.go`'s header claims it covers "ready/start → roll →
purchase → rent transfer → completed match"; the file contains two tests,
neither of which binds a second client, and its two purpose-built helpers
(`client.action`, `dialClientExpectReject`) are never called.

Three compounding effects:

1. Defects requiring two actors are structurally invisible to the suite.
2. The certification pipeline reports `CERTIFIED` for a tree it did not test
   (OPS-1) and can pass its mandatory database gate while running the wrong
   tests (OPS-2).
3. The client re-implements the economy, affordability and turn-legality rules
   because the protocol never tells it what is legal — so a divergence class
   exists that no test can currently detect.

**Thirty confirmed P0/P1 findings reduce to eight root causes.** None requires
a new gameplay system. The first patch rebuilds the evidence layer, because
until OPS and the `match` harness exist, no later fix can be certified with
trustworthy evidence.

### Severity tally

| Severity                       | Count | Areas                                                             |
| ------------------------------ | ----- | ----------------------------------------------------------------- |
| P0 critical                    | 2     | client merge invariants (CLT-1), client playability (CLT-13)      |
| P1 high                        | 28    | engine (6), multiplayer (7), persistence (5), client (8), ops (2) |
| P2 moderate                    | 31    | across all areas                                                  |
| P3 minor                       | 14    | across all areas                                                  |
| Suspected / architectural risk | 6     | §8                                                                |

---

## 3. Architecture and data-flow map

### Where authoritative state originates

```
                    HTTP lobby (unauthenticated)        WebSocket
                            │                              │
                  api/matches.go                   realtime/handler.go
                   CreateMatch / Join          DecodeClientMessage → hello/Bind
                            │                              │
                            └──────────────┬───────────────┘
                                           │
                                 match.Registry  (map, unbounded)
                                           │
                                 match.Match     ←── m.mu (one per match)
                                    ├── engine (PURE: no IO, no clock, no rand)
                                    ├── state    authoritative GameState
                                    ├── history  ring, 1024 events
                                    ├── sessions session → player → Sink
                                    └── store    persistence.Store
```

The engine is pure and the `Match` layer serializes access. The invariant
"clients are consumers of authoritative state" holds at the **engine** and at
the **ack snapshot**, and is violated in the **event reducer** (CLT-4, CLT-6).

### The transition path, end to end

```
client frame
  → DecodeClientMessage          envelope validation, seq passthrough
  → session lookup               actor = s.player   (never client-supplied)
  → seq watermark                duplicate → cached ack
  → engine.Apply(state, actor)   PURE: clone, validate, mutate, emit events
  → CommitTransition             ONE tx: events + games + snapshot   ── durability
  → m.state = next               in-memory install
  → unlock
  → Broadcast(events)            per-event conn.Write  ← NOT atomic
  → sendAck(seq, nextSeq, snap)  authoritative snapshot
```

Two things break on this path. The **durable boundary is crossed while the
lock is held** (PRS-1), and **publication is not atomic** — the transition's
events are written individually and can interleave with another transition's
(MPT-3), while a duplicate `seq` re-publishes them (MPT-2).

### How clients receive and apply results

```
event frames  ──► applyEvents  ──► reduceEvent   (client re-derives money/ownership)
ack.snapshot  ──► applySnapshot ──► wholesale replace (NO tick guard)
catchup       ──► applyEvents  ──► applied before any state exists
```

There is no dedup key, no ordering guard, and no legal-action set. This is
the whole of CLT-1/2/4/5/6, and it is why a single reordering or re-delivery
silently corrupts a match in the browser with no error surfaced.

### Duplicated rule logic (the divergence map)

| Rule                 | Authoritative implementation    | Duplicated at                              |
| -------------------- | ------------------------------- | ------------------------------------------ |
| money transfer       | `board_ruleset.go:203,264`      | `game-store.ts:161-234`                    |
| affordability        | `board_ruleset.go:188,261`      | `game-store.ts:274-282`, `App.tsx:475`     |
| whose turn / may act | `board_ruleset.go:36-60`        | `game-store.ts:264-291`, `App.tsx:436-481` |
| board geometry       | `config.go` (validated, hashed) | rendered only — no duplicate copy          |
| dice / movement      | `board_ruleset.go:68-115`       | none — correctly absent                    |

Three client re-derivations already diverge today: the client clamps money at
`Math.max(0, …)` (the server never clamps), fabricates `isHost: false` on
`player_joined` (the server sets it for the first player), and reports a lobby
departure as `'eliminated'` (the server removes the player).

---

## 4. Findings register

Classification: **DEF** confirmed defect · **GAP** coverage gap ·
**RISK** architectural risk · **DOC** documentation discrepancy.

### RC-1 — The join/seat path is neither transactional nor transport-guarded

| ID    | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ----- | --- | ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MTP-1 | P1  | DEF   | `player_join` is accepted on the WebSocket action path. The engine's join handler discards the actor, so the transition mints a **phantom player**: no token registered, no `game_players` row, but a real seat consumed from `playerCount.max`. It cannot be removed (`player_leave` requires a bindable actor), so an unauthenticated client can permanently exhaust a match's seats. `decode.go:30`, `lifecycle.go:20-21`, `match.go:223` |
| MTP-2 | P1  | DEF   | `Match.Join` assigns `m.state`, appends history and registers the token **before** `SavePlayer`, with no rollback, using the raw HTTP request context. A client hangup alone cancels the context, so the seat is consumed by a player whose token was discarded and can never bind. Repeat ⇒ `ErrGameFull`, lobby permanently unjoinable. `match.go:221-232`                                                                                 |
| MTP-3 | P1  | DEF   | `Match.Join` never calls `CommitTransition`. The primary seat-acquisition path writes **no event, no `games` update, no snapshot upsert**, directly contradicting the invariant stated at `store.go:6-7`. `LoadMatch` returns a stale snapshot that cannot explain the live roster. `match.go:229`                                                                                                                                           |
| MTP-4 | P2  | DEF   | Joins are never broadcast; existing lobby clients learn of a new seat only from a later action's ack. `game_players.money` is written once and never updated, and its `ON CONFLICT (game_id, seat) DO UPDATE SET money` would clobber a live balance. `match.go:229`, `store.go:254-256`                                                                                                                                                     |

**Root cause:** the join path was implemented as a convenience wrapper around
the engine rather than as a first-class transition with the same durability and
authorization guarantees as every other state change. **Impact:** authoritative
roster corruption and unauthenticated denial of service.

### RC-2 — Broadcast is not atomic, ordered, or idempotent

| ID     | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------ | --- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MTP-5  | P1  | DEF   | The idempotency cache stores the full `ActionResult` **including `Events`**, and `handleAction` broadcasts unconditionally. A repeated `seq` therefore **re-publishes the transition to every peer**. The server explicitly delegates dedup to the client ("its tick … is the dedup key, delivery is at-least-once", `match.go:417-421`); the client implements no dedup. One lost ack ⇒ rent charged twice, on every peer, permanently. `match.go:353`, `handler.go:166` |
| MTP-6  | P1  | DEF   | `Broadcast` writes events individually. Concurrent transitions interleave those writes, so one atomic transition's events are **not contiguous** on the wire. The client folds them in arrival order ⇒ wrong turn pointer and wrong ownership, with `Math.max(tick)` masking it. `match.go:435-450`                                                                                                                                                                       |
| MTP-7  | P1  | DEF   | The handshake takes the snapshot and the catch-up range under **two separate lock acquisitions** and writes both frames with no lock held. A client can receive `event(11) → catchup(8,9,10) → snapshot(10)`, silently losing a transition, with `resync:false` and no server-side signal. `handler.go:218-232`                                                                                                                                                           |
| MTP-8  | P2  | DEF   | Retention trims **by event count** but sets `base` to the first surviving event's tick. A cut landing inside a multi-event transition leaves a partial group, and `Since` serves it as a complete catch-up. `match.go:375-379`, `:406-408`                                                                                                                                                                                                                                |
| MTP-9  | P2  | DEF   | `Broadcast` runs synchronously on the actor's goroutine after the DB has committed, so a stalled peer delays the actor's ack by up to 3 s × peers for an action that is already durable. `exceptSeq` is a dead parameter; `Send`'s result is discarded. `handler.go:166`, `match.go:422-451`                                                                                                                                                                              |
| MTP-10 | P2  | DEF   | The sequence watermark and idempotency cache live on the **per-connection** session. Every reconnect mints a new session id, so a replayed `seq` after reconnect is applied as a **new** action. The comment claiming otherwise (`match.go:261-263`) is false. `seq = 0` is permanently rejected as stale; `seq = MaxInt64` overflows `nextSeq` and bricks the session.                                                                                                   |

**Root cause:** the transport treats a transition as a bag of independently
written frames rather than as the atomic unit the engine actually produces.
**Impact:** silent, unrecoverable client divergence; duplicate economic
application.

### RC-3 — The client merge path has no ordering or idempotency invariant

| ID    | Sev    | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ----- | ------ | ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CLT-1 | **P0** | DEF   | `pendingAction` is set on click and cleared **only** in the action-result callback. The socket-close path resolves the send promise `false` and never clears it, so the flag stays `true` permanently: every dock button disabled, "waiting for server…" shown, until a page reload — which discards the player's seat (no join-by-id, CLT-14). `App.tsx:101,132-133,417,436-483`; `connection.ts:164-176`                       |
| CLT-2 | P1     | DEF   | `applySnapshot` replaces state wholesale with **no tick comparison**. Because the server broadcasts before acking, a peer's tick N+1 can land between the actor's tick-N broadcast and tick-N ack ⇒ the client **visibly rewinds** money, ownership and turn. `Math.max` protects only the `tick` field. `game-store.ts:123-133`; `handler.go:166-167`                                                                           |
| CLT-3 | P1     | DEF   | Zero event dedup against documented at-least-once delivery. `tick` cannot serve as the key — many events share one tick and the wire carries no per-event `seq`. The moment `resumeFromTick` is wired, catch-up **double-applies every event**. `game-store.ts:136-155`                                                                                                                                                          |
| CLT-4 | P1     | DEF   | The client re-implements the entire money economy and diverges from the server in three confirmed ways: it clamps at zero (the server never clamps), fabricates `isHost` on `player_joined`, and reports a lobby departure as `'eliminated'` (the server removes the player). It also re-derives ownership and rolls it back locally on bankruptcy, and cannot clear `level`. `game-store.ts:161-234`                            |
| CLT-5 | P1     | DEF   | The protocol carries no legal-action set, so all six dock buttons are gated on client re-derivation. `Start` is enabled on `phase === 'lobby' && isHost` alone, while the server also requires min players and all-ready — the button's own hint even says "once everyone is ready". `App.tsx:442`; `lifecycle.go:168-176`                                                                                                       |
| CLT-6 | P1     | DEF   | `Decline` is gated on `canBuy`, which includes affordability, but the server's `decline` has no funds check. A legal action is unreachable; combined with any money divergence this produces a **hard stall** — `await_buy_decision` with every button disabled, no error, no way out. `App.tsx:475`; `board_ruleset.go:283-296`                                                                                                 |
| CLT-7 | P1     | DEF   | Host promotion emits no event. The successor's client keeps `isHost:false`, and `Ready` has no inverse (it only ever sends `ready:true`), so once the successor is ready the lobby is **permanently stuck**: Start disabled, Ready disabled, all board buttons disabled, server waiting for a `game_start` the UI will never send. Escape requires a reload, which creates a new match. `lifecycle.go:96-104`; `App.tsx:436-442` |
| CLT-8 | P2     | DEF   | Actions are silently dropped while disconnected (the send returns `false`, which `act()` ignores), with no user feedback. `statusDetail` is captured on error and never rendered. `nextSeq` and `cursor` are tracked then explicitly discarded (`void nextSeq`). `connection.ts:94,179-181`; `game-store.ts:132`                                                                                                                 |
| CLT-9 | P2     | DEF   | `resync` and `catchup` are dead code end to end: the cursor is never sent, the `cursor` field is ignored, catch-up is applied before any state exists, and the single Go test that claims to cover it takes a branch where `resync` is provably `false`. `connection.ts:82-90,120-124`; `multiplayer_test.go:256`                                                                                                                |

**Root cause:** the protocol never states what is legal or what has already
been applied, so the client re-derives both and has no invariant to protect
the merge. **Impact:** any reordering or re-delivery silently corrupts the
rendered match; one lost packet can end the session.

### RC-4 — Engine: confirmed correctness gaps

The engine is the most trustworthy layer in the repository, and these are the
real exceptions. Each has a regression test in v0.2.1.

| ID     | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------ | --- | ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ENG-1  | P1  | DEF   | `player_leave` during play eliminates the leaver but **never reverts their holdings** — the only elimination path that skips `bankrupt`'s sweep. The spaces stay owned by an eliminated player (violating the suite's own invariant), are **permanently unpurchasable** (`buy` and `decline` both reject `Owned`) and **permanently rent-free** (the `!owner.Active()` branch returns a quiet landing with no event). Any client can dispatch this action. `lifecycle.go:105-114` vs `board_ruleset.go:196-199,221-227` |
| ENG-2  | P1  | DEF   | `propertyRules.passingGoBonus` has **no upper bound** — the only money field without a ceiling, in both the shape and semantic validation tiers. `Money` is `int64`; with a 2-space board a 2e18 bonus overflows on a 12-total roll and a balance becomes **negative**. `config_validate.go:155`; `config_shape.go:224-230`; `board_ruleset.go:108`                                                                                                                                                                     |
| ENG-3  | P1  | DEF   | `roll` indexes `Spaces[p.Position]` unguarded. `requireTurn` validates phase, seat and actor but **not position**, and `UnmarshalGameState` does not either, so a corrupt snapshot **panics** on the next roll. `ApplyAction` has no `defer m.mu.Unlock()` — every path unlocks manually — so the panic **permanently wedges the match mutex**. `board_ruleset.go:97`; `match.go:309-355`                                                                                                                               |
| ENG-4  | P2  | DEF   | `mustParseSeed` **panics** on an empty or malformed `rootSeedHex`, which is `omitempty` and unvalidated on load. Reached from `join` and `start`, with the same mutex-wedge consequence. `lifecycle.go:231-237`; `marshal.go:28-43`                                                                                                                                                                                                                                                                                     |
| ENG-5  | P2  | DEF   | `richestActive` breaks ties in **slice** order while its comment — and the scan immediately beside it in the same function — uses **seat** order. After a lobby leave and rejoin the orders diverge and the documented lowest-seat tie-break is wrong. `board.go:112-128` vs `:156-195`                                                                                                                                                                                                                                 |
| ENG-6  | P2  | DEF   | `Clone` shallow-copies `WinnerID *PlayerID`, the same allocation the emitted `GameEndedEvent` aliases. The documented deep-copy contract is false. `state.go:50,59`; `board.go:160-182`                                                                                                                                                                                                                                                                                                                                 |
| ENG-7  | P2  | DEF   | Bankruptcy releases N properties with **no event naming them**, so the event log cannot reconstruct the post-state; the conservation test has to _assume_ the wipe. `board_ruleset.go:219-233`                                                                                                                                                                                                                                                                                                                          |
| ENG-8  | P2  | DEF   | `maxDoublesStreak: 1` makes `doublesExtraRoll: true` a silent no-op, and there is no upper bound. `player_leave` mutates a `PhaseInterrupted` (terminal) match. `game_start` returns `ErrGameFull` for too-_few_ players. `config_validate.go:242`; `lifecycle.go:74,168`                                                                                                                                                                                                                                               |
| ENG-9  | P3  | DOC   | `round_limit` ends one round _after_ the configured limit (`Round > RoundLimit`, with `Round` starting at 1). Pinned by a test and arguably intentional, but it reads as an off-by-one and no document states it. `board.go:186`                                                                                                                                                                                                                                                                                        |
| ENG-10 | P3  | DEF   | Validation issues are appended while ranging a Go map, so `ValidationError.Error()` is **nondeterministically ordered** between identical requests. `config_shape.go:31` (+8 sites)                                                                                                                                                                                                                                                                                                                                     |

### RC-5 — Durability and recovery are weaker than documented

| ID    | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ----- | --- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| PRS-1 | P1  | DEF   | `tx.Commit` can return an error **for a transaction that committed** (5 s deadline, connection loss). The code treats any error as "definitely not committed" and does `m.state = prev`, so memory falls one tick behind the database. The next transition's `INSERT` then collides on `PRIMARY KEY (match_id, tick, seq)` ⇒ **every subsequent action fails and the match is permanently bricked**. `match.go:338-347`; `store.go:151-179` |
| PRS-2 | P1  | DEF   | `Migrate` is **never called**; `Connect` verifies only `Ping` and `/ready` returns 200. Deploying against an empty database yields "database connected", ready=200, and 500s on every write. `migrate.go:66` (definition only); `main.go:74-79`                                                                                                                                                                                             |
| PRS-3 | P1  | DEF   | The per-match mutex is held across a **full DB transaction** (and `Join` across a DB write with **no deadline**), directly contradicting BACKEND.md. With `MaxConns: 8` and no `StatementTimeout`, one latency spike stalls every match for the full 5 s `ioTimeout`. `match.go:341`, `:229`                                                                                                                                                |
| PRS-4 | P2  | RISK  | `MarkInterrupted` is global and unscoped — no `instance_id`, lease, or advisory lock — so two processes sharing a database mark each other's live matches `interrupted`. Never-started lobbies are indistinguishable from crashed ones. `store.go:292`                                                                                                                                                                                      |
| PRS-5 | P2  | DEF   | Nothing is restorable: `LoadMatch` has zero non-test callers and `Registry` has no restore path. The client therefore reconnects **forever** against a match that provably cannot exist, minting a session and a permanent rate-limiter key every 8 s. `DeriveMatchSeed` is dead code whose formula cannot reproduce the engine's seed. `rng.go:101-106`; `lifecycle.go:178`                                                                |
| PRS-6 | P2  | DEF   | A no-op `player_ready` advances the tick and the `games`/`match_snapshots` cursor with **zero event rows**, so the ordered event tail cannot explain the state. `lifecycle.go:151-154`; `store.go:151`                                                                                                                                                                                                                                      |

### RC-6 — Unbounded growth and unsafe defaults, reachable unauthenticated

| ID    | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                    |
| ----- | --- | ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OPS-3 | P1  | DEF   | `Registry.matches` is **never evicted** and `POST /matches` is unauthenticated and unrated ⇒ trivial OOM plus database flooding. `match.go:169`; `matches.go:37`                                                                                                                                                                                                                                           |
| OPS-4 | P1  | DEF   | The process-global `RateLimiter.hits` map is **never swept** — `Allow` prunes only the key being queried — and is keyed by never-reused session ids ⇒ **~560 B retained permanently per connection ever opened, ≈480 MB/day** at 10 conn/s, unreachable by any code path. `match.go:489-511`; `handler.go:51`                                                                                              |
| OPS-5 | P1  | DEF   | `InsecureSkipVerify = true` whenever `HIGHJACK_ALLOWED_ORIGINS` is unset, **in any environment**. `config.DevMode()` exists for exactly this purpose and has **zero call sites**; the variable has no default. Contradicts BACKEND.md. `handler.go:76-81`; `config.go:67-73,108-110`                                                                                                                       |
| OPS-6 | P2  | DEF   | `Match.sessions` entries are never deleted; each retains a socket and, once used, a full `GameSnapshot`. No connection limit exists. The WS origin allow-list has **zero test coverage**. `game_started` broadcasts the **match seed** to every client, contradicting `snapshot.go:55-57`. Token hashes are compared with `==`. `match.go:282`; `handler.go:67-72`; `lifecycle.go:201`; `match.go:270-277` |
| OPS-7 | P2  | DEF   | A valid token mints unlimited simultaneous bound sessions, each with an independent watermark, and the previous connection is never superseded. `Bind`'s rebinding branch computes `dup` and then does nothing (`_ = existing`). `match.go:247-262`                                                                                                                                                        |

### RC-7 — Product gaps that make the advertised game unreachable

| ID     | Sev    | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------ | ------ | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CLT-13 | **P0** | DEF   | The entire match-information panel is `hidden md:flex`. At a 412 px viewport a player **cannot see their chip balance, whose turn it is, the round, or the player list** — while being asked to make buy/afford decisions whose only price hint is a `title` attribute iOS does not show on tap. The e2e "mobile" test stops in the lobby, which is why this ships green. `App.tsx:288-290`; `game.spec.ts:160-168` |
| CLT-14 | P1     | DEF   | There is **no join-by-id path**. `?match=` is written by `replaceState` and **never read**; there is no join form, no token storage, and `joinMatch` is only ever called with an id the same user just created. The advertised two-player flow is reachable only through the raw `page.evaluate` WebSocket in the e2e test, whose file header claims "Nothing is mocked". `App.tsx:114-129`; `game.spec.ts:84-111`  |
| CLT-15 | P1     | DEF   | The client **never pings**, while the server enforces a 90 s read-idle timeout and advertises a `heartbeatMs` it never uses. Every idle match is disconnected by the server, and each drop mints a new session plus a new permanent rate-limiter key. `handler.go:33,206,236-244`; `connection.ts`                                                                                                                  |
| CLT-16 | P2     | DEF   | `resumeFromTick: 0` with empty history sends **nothing** and sets `resync:false` — the fallback is gated on `cursor > 0`, exactly inverted from the case that needs it. `handler.go:226-230`                                                                                                                                                                                                                        |

### RC-8 — Certification, CI, and documentation integrity

All four were **reproduced**, not inferred. Reproduction commands are in §6.

| ID     | Sev | Class | Finding                                                                                                                                                                                                                                                                                                                                                                                            |
| ------ | --- | ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OPS-1  | P1  | DEF   | The report records `git rev-parse HEAD` — the **last commit, not the certified tree**. A dirty tree certifies under a commit that was never tested. This is exactly how `v0.2.0` shipped with a report naming `7ec826b` while the tree was `494ac57`. `certify.ts:19-22,39,69`                                                                                                                     |
| OPS-2  | P1  | DEF   | CI's database gate uses `-run 'TestMigrations\|TestMatch\|TestCommit\|TestPlayer'`. It **excludes** `TestCreateAndLoadMatchRoundTrips` (the suite's stated purpose) and `TestMarkInterruptedFlags` (the only restart-policy test); `TestMatch` matches **no test at all**; and `go test` exits 0 on a zero-match filter, so a rename silently converts a mandatory step into a no-op. `ci.yml:102` |
| OPS-8  | P1  | DEF   | axe scans six **marketing** routes. The game client is served from a different origin and is **never scanned**; the only keyboard test is a marketing skip link. The entire product surface sits outside the mandatory accessibility gate. `accessibility.spec.ts:4`; `smoke.spec.ts:60-65`                                                                                                        |
| OPS-9  | P2  | DEF   | `certify` omits `bun audit`, `govulncheck`, `-race`, the database gate and the container gates that CI runs — 14 gates in the report against 22 in CI. Database tests skip while `test:go` is recorded `passed`. `HIGHJACK_SKIP_BROWSER=1` yields **exit 0**. `validation-gates.ts:28`; `certify.ts:33,58-75`                                                                                      |
| OPS-10 | P2  | GAP   | `server/internal/match/` has **no test file**. 511 lines containing `Bind` (the entire authorization boundary), `ApplyAction` (idempotency + durability ordering), `Broadcast`, `Since` and `RateLimiter` are untested. This gap is why RC-2 and RC-5 were findable by reading but invisible to `go test`.                                                                                         |
| OPS-11 | P2  | GAP   | `TestServerFixturesShapeMatchGoEncoding` marshals a map **constructed inside the test** and compares it to the fixture; it never touches production encoding, so it cannot fail for any production reason. 7 of 10 message fixtures and **all 12 event fixtures** are verified by no Go test. `protocol_compat_test.go:100-111`                                                                    |
| OPS-2b | P2  | DEF   | `multiplayer_test.go`'s header documents coverage that does not exist: two tests, neither binding a second client, with two purpose-built helpers never called. `multiplayer_test.go:25-31,204,223`                                                                                                                                                                                                |
| OPS-12 | P2  | DOC   | `TESTING.md` cites `server/internal/match/match_test.go` (does not exist) and claims jsdom + `@solidjs/testing-library` for `apps/game`, which uses `environment: 'node'`, has neither dependency, and no `setup.ts`. **None of the 525 lines of UI are tested.**                                                                                                                                  |
| OPS-13 | P2  | DOC   | BACKEND.md claims the mutex is "never held across a network write or a database call" (false: PRS-3), that "integration tests drive two real WebSocket clients" (false: OPS-2b), that an unlisted origin can call "neither the lobby nor the socket" (false: OPS-5), and that `resumeFromTick` replays retained events (false: CLT-9).                                                             |

---

## 5. Root-cause analysis and dependency relationships

The eight clusters reduce to **four** underlying causes.

**Cause A — the evidence layer does not exist where the risk is.**
`internal/match` has no tests (OPS-10) and no two-client integration test
(OPS-2b), so every concurrency-, ordering- and authorization-adjacent defect
is invisible. _Blocks verification of:_ MTP-1…10, PRS-1, PRS-3.
_Fix first,_ because the harness is what proves the later fixes.

**Cause B — durability and authorization are applied at the wrong boundary.**
`Join` was written as a wrapper rather than a transition (MTP-2/3), the WS
transport forwards every action type including the one that mints identity
(MTP-1), and the commit outcome is treated as binary when it is not (PRS-1).
These share a fix site: a single place where "authorize → apply → persist →
publish" happens for **all** state changes.

**Cause C — the transport publishes a bag of frames, not a transition.**
MPT-5, MTP-6, MTP-7, MTP-8 are one bug in four places: the atomic unit the
engine produces is destroyed at the publish boundary. Fixing them as one change
(per-transition batched frame, single per-match fan-out, ordered handshake,
tick-aligned retention) is narrower and more sound than four local patches.

**Cause D — the protocol does not state what is legal or what was applied.**
CLT-2, CLT-3, CLT-5, CLT-6, CLT-7 all follow from the client having to
re-derive affordability, turn eligibility, and its own position in the event
stream. A server-computed `legalActions` set plus an explicit event-identity
contract removes the whole class rather than each symptom.

**Dependencies:**

```
Cause A (harness) ──► Cause B (join/authorization/durability)
                   ──► Cause C (publish atomicity)
                          │
Cause D (legalActions + event identity) ──► client merge invariants (CLT-2/3/5/6/7)

OPS-1/2/9 (evidence integrity) ──► gates every release's trustworthiness
```

Cause D's `legalActions` must be produced by the **engine**, not the match
layer, or it becomes a fourth copy of the rules and reintroduces the
divergence it removes. The engine already computes every precondition in
`requireTurn`; this is a projection, not new logic.

---

## 6. Reproduction evidence

### OPS-1 — a dirty tree certifies under an untested commit

```sh
printf '\n<!-- drift -->\n' >> docs/development/VALIDATION.md
bun run certify           # → exit 0
python3 -c "import json;print(json.load(open('verification/report.json'))['commit'])"
# → 494ac57c40d9deb28dc73e6b6d2d3b76685dd7d3
test -n "$(git status --porcelain)" && echo "tree was dirty"
# → tree was dirty
```

### OPS-2 — CI's database gate silently drops the round-trip and restart tests

```
MATCH  TestMigrationsApplyIdempotently
MATCH  TestCommitTransitionPersistsStateAndEvents
MATCH  TestCommitTransitionRollsBackOnFailure
MATCH  TestPlayerTokenResolvesOnlyWithinItsMatch
SKIP   TestCreateAndLoadMatchRoundTrips     ← the suite's stated purpose
SKIP   TestLoadMatchMissingReturnsErrNoMatch
SKIP   TestMarkInterruptedFlags            ← the only restart-policy test
SKIP   TestLoadMigrationsRejectsBadNames / IgnoresNonSQLFiles /
       RejectsDuplicateVersions / SortsByVersion
```

`TestMatch` matches none of the nine names.

### OPS-9 — a skipped browser gate still exits 0

```sh
HIGHJACK_SKIP_BROWSER=1 bun run certify     # → exit 0, classification VALIDATED
```

`CERTIFICATION.md` step 3 requires exit 0 to certify, so any pipeline gating
on the exit code accepts a build with zero browser verification.

### OPS-10 / OPS-2b — the authorization layer has no tests

```sh
ls server/internal/match/     → match.go        (no _test.go)
grep -c "^func Test" server/internal/realtime/multiplayer_test.go  → 2
```

Both tests bind exactly one client; `client.action()` and
`dialClientExpectReject()` have no call sites.

---

## 7. Coverage gaps and verification weaknesses

| Layer                          | Claimed                                                  | Actual                                                                                |
| ------------------------------ | -------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `internal/match`               | authorization, idempotency, durability order, broadcast  | **no test file**                                                                      |
| Two clients in one match       | "the real multiplayer integration test"                  | **none exists**                                                                       |
| Protocol fixtures              | "the two implementations cannot drift apart silently"    | 3/10 message fixtures, **0/12 event fixtures**, 0/8 action, 0/5 config verified by Go |
| Go fixture parity test         | "identical field sets and values as the shared fixtures" | marshals a map built in the test; cannot fail for a production reason                 |
| Game client UI                 | jsdom + testing-library, "accessibility contract"        | `environment: 'node'`, no deps, **0 tests of 525 UI lines**                           |
| Game e2e, player B             | "two distinct WebSocket clients"                         | raw `page.evaluate` socket; never renders the UI                                      |
| Game e2e, mobile               | viewport coverage                                        | stops in the lobby; never enables a control                                           |
| E2E assertions                 | meaningful                                               | `toHaveText(/=(2                                                                      | 3   | …   | 12)$/)` accepts **any** possible roll; tick assertions also pass on a rejected roll |
| Accessibility                  | "axe scans pass on key pages"                            | marketing routes only; the game origin is never scanned                               |
| `resync` / `catchup`           | reconnect recovery                                       | dead code end to end; the one Go test takes the branch where `resync` is `false`      |
| Database tests under `certify` | green                                                    | silently `t.Skip`, recorded `passed`                                                  |

---

## 8. Risks accepted or deferred, with rationale

| ID    | Item                                                               | Disposition & rationale                                                                                                                                                                                                                                                                                                         |
| ----- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RSK-1 | `MarkInterrupted` is unscoped (PRS-4)                              | **Suspected, not confirmed.** Provable that no `instance_id`/lease/advisory lock exists; the deployment topology is not determinable from the repository. v0.2.2 documents the single-instance constraint explicitly and does **not** add a lease mechanism speculatively. Needs a topology answer before it becomes a finding. |
| RSK-2 | `GET /matches/{id}` is unauthenticated and returns a full snapshot | **Accepted.** Mitigated by 64-bit unguessable ids and `Cache-Control: no-store`; no practical break was constructible. Documented as a metadata oracle if an id ever leaks. Changing the API surface without direction is worse than recording the risk.                                                                        |
| RSK-3 | One turn can stall the match indefinitely (no server turn timer)   | **Accepted for v0.2.x.** The engine state machine is correct; liveness above it is a product decision (auto-pass, timer, or forfeit) that would change documented gameplay, so it belongs to a gameplay milestone, not a patch.                                                                                                 |
| RSK-4 | `IntN` modulo bias (~2.3e-19 for n=6)                              | **Accepted.** Documented in-source as acceptable for game use, explicitly deferred to "gambling-grade fairness". No gambling subsystem ships in v0.2.x.                                                                                                                                                                         |
| RSK-5 | Retirement of the board loop's `RuntimeSpace.Level` field          | **Accepted.** It is inert but reserved for property development, which GAME_DESIGN.md already documents as directional. Removing it would churn the config hash contract.                                                                                                                                                       |
| RSK-6 | `player_ready` no-op burns a tick (PRS-6)                          | **Fixed in v0.2.2** as part of the event-stream work, not deferred — it interacts with the reconciliation added for PRS-1.                                                                                                                                                                                                      |

---

## 9. Patchwork version assignments

Derived from the findings, not from the proposed skeleton. **The ordering
deviates from the original proposal in one respect:** the certification
evidence layer is repaired in v0.2.1 rather than v0.2.4, because the engine
fixes in the same patch are only trustworthy if the pipeline can prove what it
tested, and because every later transport fix is written against the
`internal/match` harness that v0.2.1 introduces.

### v0.2.1 — Evidence integrity & engine correctness

- **Addresses:** OPS-1, OPS-2, OPS-9, OPS-10, OPS-11, OPS-2b, OPS-12; ENG-1…6, ENG-8, ENG-10.
- **Excludes:** all transport and realtime behavior changes.
- **Compatibility:** no protocol or schema change. No migration.
- **Acceptance:** every P1 engine defect closed with a deterministic
  regression test; every gate in the report is actually executed; the report
  identifies the certified tree; `certify` and CI run one shared gate list.

### v0.2.2 — Multiplayer integrity & durability

- **Addresses:** MTP-1…10; PRS-1…6; OPS-3…7.
- **Depends on:** v0.2.1's harness — repro tests land here, failing first.
- **Compatibility:** additive snapshot field for durability status; a new
  migration only if a schema change proves unavoidable (none is expected —
  PRS-2 is a boot-ordering fix, not a schema fix).
- **Acceptance:** two independent clients demonstrably converge on identical
  authoritative state; no unauthenticated path can exhaust a match, the
  registry, or process memory; every documented recovery guarantee is either
  proven by test or corrected in the docs.

### v0.2.3 — Client correctness & playable experience

- **Addresses:** CLT-1…9, CLT-13…16; OPS-8.
- **Protocol:** additive `legalActions: ActionType[]` on the snapshot;
  `PROTOCOL_VERSION` 1.1.0 → **1.2.0** (major stays 1, schema stays 1,
  backward compatible — an older client ignores the field, a newer client
  requires nothing the old server lacks, falling back to re-derivation).
- **Acceptance:** no misleading or permanently dead control; the supported
  match loop is playable end to end on desktop and mobile; the browser gate
  covers the real product with two real clients.

### v0.2.4 — Accessibility, hardening & documentation truthfulness

- **Addresses:** the remainder — ENG-7, ENG-9; CLT-11, CLT-12, CLT-17…21;
  MTP-4, PRS-4, PRS-5, OPS-6, OPS-13; RSK-1…5 documentation.
- **Acceptance:** every accepted finding is either fixed or recorded with
  rationale; no document describes behavior the code does not have.

---

## 10. Implementation order and acceptance criteria

Per patch: reproduce → failing regression test → smallest root-cause fix →
focused tests → dependent + integration tests → diff review for unrelated
changes → documentation → full `bun run certify` → confirm the report names
the intended commit → commit and push.

Held throughout: no weakening of tests, no silent skips, no new `t.Skip`, no
edits to historical migrations, no movement of `v0.2.0` or `v0.1.0`, no
history rewrite, no v0.3.0 feature work.

**Release certification gate for every patch:** exit 0; report
`classification: CERTIFIED`; report commit equals `git rev-parse HEAD`; report
`dirty` absent; `skippedGates` empty or explicitly justified; the same gate
list as CI.
