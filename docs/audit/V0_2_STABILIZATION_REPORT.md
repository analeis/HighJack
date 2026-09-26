# HighJack v0.2.x Stabilization Report

Status: **complete**. Four patch releases, each certified against the tree it ships.

This report closes the post-v0.2.0 audit cycle. It records what was found, what was
fixed, what was deliberately deferred, and — as importantly — where the process
itself was wrong.

## Releases

| Version | Commit       | Scope                                                                     |
| ------- | ------------ | ------------------------------------------------------------------------- |
| v0.2.0  | `494ac57`    | Board loop (baseline for this cycle)                                      |
| v0.2.1  | `524c6ec`    | Evidence integrity, engine correctness, first real two-client tests       |
| v0.2.2  | `8454f93`    | Multiplayer integrity, durability, resource and origin hardening          |
| v0.2.3  | `82dfac7`    | Server-owned legality, playable client, and a P0 regression in v0.2.1     |
| v0.2.4  | this release | Accessibility, and a CI pipeline that could agree with itself while lying |

`v0.1.0` (`8f2fec1`) is untouched. Every release was certified with 17 gates, zero
skipped, and a report whose `commit` and `tree` both equal `HEAD`.

## What the cycle found

The audit normalized 30+ findings across engineering, multiplayer, persistence,
client and operations. The ones that mattered most were not the ones predicted:

### The P0 that survived two certified releases

`v0.2.1` changed `Match.store` from a concrete `*persistence.Store` to a `Store`
interface. **A typed nil pointer stored in an interface is not nil.** Every
`store != nil` guard in the match package therefore passed when no database was
configured, and `POST /matches` returned **500** — the game could not create a match
at all in normal local development.

It shipped, and was certified, twice. Two independent causes had to be removed
before it could surface:

1. **No test exercised the nil case.** Every Go test injects a fake store, so the
   one configuration that mattered most was the one configuration never tested.
2. **The browser gate tested a stale server.** `reuseExistingServer: !process.env.CI`
   reused whatever already held port 8080 — locally, a v0.2.0 binary. The game suite
   was asserting against pre-audit rules and reporting success.

The second cause is the serious one, and it is a lesson about evidence rather than
about Go. The v0.2.1 certification work made the report name the exact tree under
test. That is necessary and it is not sufficient: **naming the tree does not protect
against a stale process.** The harness itself had to stop reusing servers, and the
`repo` gate now fails if CI does not enforce every gate the pipeline defines.

CI would have caught it — `CI=true` makes reuse false. But local certification, the
thing a developer actually runs, was providing false confidence. That is the failure
mode this whole cycle exists to eliminate, and it survived the first fix aimed at it.

### Client defects that were structural, not cosmetic

- **The client re-implemented the rules.** Affordability, turn ownership and start
  eligibility existed in four places in the client and in the engine, and had
  drifted. The result was a Start the server refused, a Decline the server accepted,
  and a turn where every control was disabled with no error. Legality is now
  computed once, by the engine, and sent per recipient.
- **A dropped connection wedged the table.** The pending-action flag was cleared
  only by an acknowledgement, and a socket closing mid-action never sends one. Every
  button stayed disabled behind a permanent "waiting for server…" until a reload,
  which then discarded the player's seat. A phone player additionally had no chip
  balance, no turn indicator, and a property price available only as a `title`
  attribute iOS does not display on tap.
- **The two-client test could not see any of it.** It drove a raw WebSocket and
  asserted on a hand-built store. It never rendered a dock, a store or a canvas, so
  every defect above was structurally invisible to it. It is now two real browsers
  on the real interface, on both viewports.

### Accessibility was worse than "missing"

- The board was a canvas whose accessible name described its _rendering method_.
  A screen-reader user learned that the board was "rendered from authoritative match
  state" and nothing else — no dice, no prices, no positions, no turn.
- **Reduced motion removed playability, not just animation.** The active player was
  indicated by a pulse, and the ticker is not registered at all under
  `prefers-reduced-motion: reduce`. Those users were given a board with no
  indication of whose turn it was.
- Turn and ownership were colour-only.
- Every `role="status"` region was mounted together with the message it was meant to
  announce, so it announced nothing. A live region must be in the accessibility tree
  _before_ its content changes.

## Backend CI: a question worth asking

The backend job invoked its Go gates through `bun scripts/gate.ts`. That was wrong
twice over: `go vet` and the race detector are not a JavaScript concern, and the job
**never ran `bun install`** — the runner worked only because that file happens to
have no external imports. Any Bun or Node regression would have failed the Go build
for no reason.

The gates are now written directly in the toolchain each job already has. That
removes the runtime dependency, and it introduces a real risk: the two definitions
can drift, which is exactly how CI once came to skip the tests it was named after.
The mitigation is mechanical rather than documentary — `repo sanity` fails when a
gate defined in `scripts/lib/validation-gates.ts` is not enforced by the workflow.
The check was verified by removing the `test:go` step and watching it fail.

## Deferred, with reasons

| Finding                                                        | Why deferred                                                                                                                                                                                                                        |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **RSK-1** — `MarkInterrupted` has no instance or lease scoping | Correct for one instance, incorrect for several sharing a database. The deployment topology is not knowable from the repository, and guessing would have been worse than documenting it. Needs an answer before horizontal scaling. |
| **PRS-3** — the match mutex is held across the database commit | Bounded at 5s and correct for a single instance. Fixing it properly needs per-match single-flight, which is a design change rather than a patch. Documented in `BACKEND.md` rather than papered over.                               |
| Protocol 1.1 compatibility shim for `legalActions`             | The client falls back to deriving legality against a 1.1 server. Acceptable while both ends ship together.                                                                                                                          |
| `resumeFromTick` client wiring                                 | The server sends it; the client reconnects from a fresh snapshot instead. Correct, slightly less efficient.                                                                                                                         |

These are stated in the documentation rather than left for a reader to discover.

## Process changes that outlive this cycle

1. **Certification names its tree** and refuses a dirty one.
2. **Skipped gates are failures**, and the browser gate cannot be skipped.
3. **No test filter may match zero tests** in CI.
4. **The browser harness never reuses a game server or preview.**
5. **CI must enforce every gate the pipeline defines**, checked mechanically.
6. **Schema-scoped, re-runnable database tests**, so they are order-independent.
7. **Accessibility is scanned on a live board on the real game origin.**

## Honest limitations

- Docker was unavailable throughout, so PostgreSQL gates used local binaries. The
  container build and its health smoke test are exercised in CI only and have not
  been run here.
- The database gate has never been observed running concurrently with itself; the
  single-flight work in RSK-1 remains the known limit.
- Accessibility work is verified with axe plus explicit assertions on the specific
  defects found. Axe cannot tell whether the board _reads_ well, only whether it is
  structurally valid.
- Multi-instance behaviour is undefined and untested. This is the largest open risk
  in the codebase and it is a design question, not a bug.

## Conclusion

v0.2.x is stable for a single instance and is honestly documented as such. The board
loop is playable on a phone, legible to a screen reader, and correct under a dropped
connection. The rules live in one place.

The most valuable outcome was not a bug fix. It was discovering that a green
certification had been verifying a stale binary for two releases — and that the
first fix for that class of problem, while necessary, was not sufficient.
