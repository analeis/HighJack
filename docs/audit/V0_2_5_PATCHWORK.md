# HighJack v0.2.x Post-Review Patchwork

Status: **in progress.** Baseline: the v0.2.4 code review (`docs/audit/V0_2_4_CODEBASE_REVIEW.md`).

The review produced a findings register. This document tracks how each finding
is dispositioned, so an external reviewer can see what was fixed, what was
deferred, and why — without reading the diffs.

## Rules for this cycle

1. **Demonstrated correctness defects first.** A finding is worked only when it
   has been reproduced, not merely reasoned about.
2. **Every fix carries a regression test** that fails without it. A test that
   cannot fail is not evidence.
3. **Every release is certified.** 17 gates, no skipped gates, report naming the
   certified tree.
4. **No oversized refactors.** The register is worked in small releases, and
   architectural questions are not answered by a patch.

## Explicitly out of scope for patchwork

Two findings from the register are **deployment and concurrency decisions**, not
defects. They are recorded, not refactored:

- **RSK-1 — multi-instance correctness.** `MarkInterrupted` has no instance or
  lease scoping, so a second instance sharing one database would mark the first
  instance's live matches interrupted. Whether HighJack may ever run more than
  one instance against one database is a deployment question the repository
  cannot answer, and guessing would be worse than documenting it. It stays
  open, and it is the largest known risk in the codebase.
- **PRS-3 — the match mutex is held across the database commit.** Correct for a
  single instance, bounded at 5 s, and documented in `docs/architecture/BACKEND.md`.
  Fixing it properly needs per-match single-flight, which is a design change.

## Register

| ID    | Finding                                                              | Severity        | Disposition                     |
| ----- | -------------------------------------------------------------------- | --------------- | ------------------------------- |
| CRT-1 | Retention window split a transition                                  | HIGH            | **Fixed in v0.2.5**             |
| CRT-2 | A full registry reported as 500                                      | HIGH            | **Fixed in v0.2.5**             |
| CRT-3 | A chunked request's config was ignored                               | LOW             | **Fixed in v0.2.5**             |
| TST-1 | Two tests failed at random                                           | HIGH (evidence) | **Fixed in v0.2.5**             |
| CAP-1 | The registry never evicts; the lobby is unrated                      | HIGH            | **Fixed in v0.2.6**             |
| TRC-1 | No build path produces an identifiable binary                        | MEDIUM          | **Fixed in v0.2.7**             |
| TRC-2 | README, manifests and client UI name the wrong version               | MEDIUM          | **Fixed in v0.2.7**             |
| PRO-1 | The TS protocol requires a `seed` the server strips                  | MEDIUM          | **Fixed in v0.2.8**             |
| PRO-2 | Parity tests compare vocabulary, not field shapes                    | MEDIUM          | **Fixed in v0.2.8**             |
| TOK-1 | The stored seat is never cleared and is not per-match                | MEDIUM          | **Fixed in v0.2.9**             |
| CER-1 | The ESLint gate needs an unprovisioned root install                  | MEDIUM          | **Fixed in v0.2.9**             |
| CER-2 | `scripts/` is type-checked but never linted                          | LOW             | **Fixed in v0.2.9**             |
| LOW-1 | `Broadcast`'s `exceptSeq` is unused                                  | LOW             | **Fixed in v0.2.9**             |
| LOW-2 | Dead block in `Bind`                                                 | LOW             | **Fixed in v0.2.9**             |
| LOW-3 | The `config` package has no tests                                    | LOW             | **Fixed in v0.2.9**             |
| LOW-4 | The migration runner ignores a late-added lower version              | LOW             | **Fixed in v0.2.9**             |
| LOW-5 | The `repository` CI job installs 518 packages to run two scripts     | LOW             | **Fixed in v0.2.9**             |
| MOD-1 | The website was never linted: ESLint skipped every `.astro` file     | HIGH            | **Fixed in v0.2.10**            |
| MOD-2 | The history trim allocated ~11 KB per accepted action                | MEDIUM          | **Fixed in v0.2.10**            |
| MOD-3 | The game client was one 926-line file                                | LOW             | **Fixed in v0.2.10**            |
| MOD-4 | The match runtime was one 1029-line file                             | LOW             | **Fixed in v0.2.10**            |
| MOD-5 | The Solid `ref` idiom was inconsistent across four sites             | LOW             | **Fixed in v0.2.10**            |
| MOD-6 | Dev mode relaxed the socket origin check but not the HTTP CORS check | MEDIUM          | **Fixed in v0.2.10**            |
| MOD-7 | Dead exported surface: `Match.Resume`, `Store.SavePlayer`            | LOW             | **Fixed in v0.2.10**            |
| RSK-1 | Multi-instance correctness                                           | —               | **Open — deployment decision**  |
| PRS-3 | The match mutex is held across the commit                            | —               | **Open — concurrency decision** |

## v0.2.5 — correctness (shipped)

The first two demonstrated correctness defects in the register, plus the
test-quality defects the review surfaced indirectly, plus the dependency
advisories that were blocking every release.

### What was actually wrong

**The retention window split transitions.** `commitLocked` — the path every roll,
purchase and turn change takes — trimmed the retained event history by raw event
count, while the join path trimmed on tick boundaries and documented at length
why counting is wrong. One transition emits several events that share a tick,
and the tick is the transition's identity on the wire, so a count-based cut
retains the _tail_ of a transition. `Since` then serves that tail as a complete
catch-up, and a client applies a purchase without its payment, or a turn advance
without the roll that caused it.

The cut lands mid-group only sometimes, so the defect was intermittent. Neither
path had a test: the correct behaviour was asserted only in a comment, on the
path that was already correct.

**A full registry reported as a server fault.** `POST /matches` mapped
`ErrRegistryFull` to 500. It is a capacity condition — the process is working
correctly and declining more work — and reporting it as a fault told an operator
reading the logs that the server was broken.

**A chunked request's config was silently ignored.** The body was read only when
the request declared a `ContentLength`. A chunked request declares none, so every
streaming client received the default board while the response reported success.

### The finding that was not in the register

Two tests asserted things that are false for some random values:

- `TestAckSnapshotCarriesViewerLegalActions` asserted that a roll is never
  offered again. That is untrue when the dice are a double — a double grants an
  extra roll, and a double happens on one roll in six.
- `TestTokenLookupIsExactAndRejectsMutations` "mutated" a random hash by
  replacing its first character with a fixed `"0"`, which is not a mutation at
  all when the hash already begins with `"0"`. Roughly one hash in sixteen.

Together these made **about one certification run in five fail spuriously**. A
gate that fails at random undermines every green result it has produced,
including the ones that happened to be correct — and it means the review's own
"the certification discipline is sound" claim needed qualifying.

Both are now asserted as the invariants they actually are, and the whole Go
suite passes ten consecutive runs.

### Also in this release

`bun audit` failed with 30 advisories in 9 transitive packages, all published
since the previous certification. `bun audit fix` cleared 28 by upgrading within
their existing ranges; the last two are `seroval`, whose critical advisory is
only fixed outside the range `solid-js` declares, so `seroval` joins the
repository's existing `overrides` block.

## v0.2.6 — capacity (shipped)

The substantive half of the registry finding. v0.2.5 made the _refusal_ honest;
this makes the condition unreachable in the first place.

### The problem

`POST /matches` is unauthenticated and unrated, and the registry had a bound but
**no eviction**. So a caller could fill it to 512 matches and keep it full:
every later request was refused, and because nothing was ever removed, the
refusal was permanent for the life of the process. The bound was not a safety
limit; it was a denial of service any script could trigger.

The backend notes claimed the registry was "bounded and swept". It was bounded.
It was never swept. The notes also claimed the match mutex being held across the
database commit was "fixed in v0.2.2" — it was not, and it is still not.

### The fix

Two bounds, because either alone was insufficient.

- **Sweep.** A match with no connected client that has not changed for
  `HIGHJACK_MATCH_IDLE_TTL` (default 1h) is evicted every minute. A match with
  a connected client is **never** evicted, however long it sits, so a slow game
  is never reclaimed out from under its players. The TTL measures _inactivity_,
  not age: an action refreshes it.
- **Rate limit.** `POST /matches` and `POST /matches/{id}/players` share a
  sliding-window budget of `HIGHJACK_LOBBY_RATE_LIMIT` requests per
  `HIGHJACK_LOBBY_RATE_WINDOW` (default 60 per minute), keyed by remote
  address. The WebSocket transport already rate-limited actions; the HTTP
  surface did not.

The sweep alone would have left the registry unbounded in principle, and the
bound alone is what made the exhaustion permanent. Together, an unauthenticated
caller can neither grow process memory without limit nor make the lobby stop
working.

### A deliberate limitation

The rate-limit key is the remote address. A deployment behind a proxy sees the
proxy's address for every client, so the limit applies to the proxy as a whole.
That is stricter than intended, never looser, and safe by default. Trusted-proxy
handling would need a separate, explicit configuration and is not introduced
here.

### Also in this release

The `config` package had no tests at all — the one place where a typo in an
environment variable turns into a server that behaves differently from the one
the operator intended. The capacity knobs are now covered.

## v0.2.7 — traceability (shipped)

A reader of this repository, a reader of the running server, and the person
holding a deployed container all had different ideas of which release they were
looking at.

### What was actually wrong

- Every workspace manifest said `0.1.0`.
- The game client's badge said `v0.2.0` — two releases stale.
- The tags were at `v0.2.4`.
- The README described the project as a foundation "not yet the full game".
- The server binary reported `dev` and `unknown` from `/version`, because
  `version.Version` and `version.Commit` default to those and **no build path
  in the repository ever overrode them**. The Dockerfile accepted the metadata
  as build arguments; nothing ever passed them. The endpoint was well designed
  and could never identify anything.

### The fix

`VERSION` at the repository root is now the single source of truth, and
everything that states a version reads it:

| Consumer                     | How it gets the version                                     |
| ---------------------------- | ----------------------------------------------------------- |
| The five workspace manifests | `repo sanity` fails if they disagree with `VERSION`         |
| The game client's badge      | injected at build time by Vite's `define`                   |
| The server binary            | `-ldflags` in `scripts/build-server.sh`                     |
| The container image          | `--build-arg VERSION` / `COMMIT`, now actually passed by CI |
| The release tag              | `git tag -a "v$(cat VERSION)"` — derived, not typed         |

The checks are mechanical, not conventional. `repo sanity` fails when the
manifests, the client, or the version disagree, and when the nearest reachable
tag is _ahead_ of `VERSION` — the case that means the version was rolled back
below a release that already exists. Each was verified by breaking it.

The browser-test server is now built by the repository's own build script rather
than by `go run`, so the server the tests exercise is the server the release
ships, version and commit included. `go run` left both unset.

### The client's badge is now visible everywhere

It was `hidden sm:inline`, so a phone player — the audience the mobile work in
v0.2.4 was for — could not see which build they were on at all. It is now
visible on every viewport, shortened on narrow ones.

### Regression tests

Two browser-level tests, both of which fail when the fix is reverted:

- the client's badge matches `VERSION`;
- the server's `/version` reports `VERSION` and a real commit.

## v0.2.8 — protocol correctness (shipped)

The two halves of the protocol finding, plus a third divergence the new check
found on its first run.

### What was actually wrong

**The contract required a field the server removes.** `packages/protocol`
declared `GameStartedEvent.seed` as required, and `isGameEvent` therefore
rejected every `game_started` event the server sent. The server removes the
seed deliberately: it derives every future roll from it, so publishing it would
let any participant predict the rest of the match.

The client did not notice, because it never validates events — it casts them.
The defect was in the _exported contract_, which is what a second
implementation, a generated client, or an external reviewer would read.

**The contract omitted a field the server sends.** `player_bankrupt` carries
`releasedSpaces`, the board spaces that reverted to the bank. Without it the
bank absorbs property value with no event naming it, so the log cannot explain
the post-state and a replay cannot reconstruct ownership. The contract never
described it.

**The client inferred what the event states.** On bankruptcy the client
reverted every space the player owned rather than the ones the event names.
The two agree today, and the new field makes the distinction checkable.

### The fix

- The contract no longer declares a seed, and no longer requires one.
- The contract now describes `releasedSpaces`, and the client uses it.
- The shared `game_started` fixture no longer claims a field the wire lacks.

### Field-level parity

The existing parity suite compares _names_ — event types, error codes, phases,
action types. It could not see either divergence above, because both are about
the _shape_ of an event rather than its name.

A new suite compares the shape of every event as it appears on the wire against
the contract, with two rules:

- every field the server puts on the wire must be described by the contract;
- every field the contract _requires_ must be one the server sends.

It encodes a **fully populated** sample of each event with the production
encoder, so the encoder's removals and `omitempty` are accounted for by
construction rather than by a special case. The first version of this test used
zero-valued samples and reported a divergence that was not one — an
`omitempty` field is invisible in a zero value. That is recorded here because
it is the kind of mistake a field-level test invites, and the fix is to make
the sample carry the field rather than to relax the assertion.

The TypeScript contract also carries a compile-time assertion that it does not
declare a seed. If that field is ever added back, the build fails on the line
that caused it, rather than on a runtime assertion somewhere else.

### Regression tests

- `TestEventFieldsMatchTypeScript` — the wire shape against the contract.
- `TestGameStartedCarriesNoSeed` — the security property, named as such.
- `TestTypeScriptDoesNotRequireTheSeed` — the contract half.
- `isGameEvent` accepts the event the server actually sends, and still accepts
  a stray seed from an older peer.
- The client reverts exactly the spaces a bankruptcy event names.

## v0.2.9 — lifecycle and hygiene (shipped)

The last of the register: the seat lifecycle, the certification pipeline's own
quality, and the small defects.

### The seat lifecycle

The reconnect token is a bearer credential. The server keeps only its digest;
the client keeps the secret. It never expires and is never revoked. How the
client stores it is therefore a security question, and the answer was:

- one global key, so joining a second table discarded the first seat;
- a `forgetSeat` function that existed and was called from nowhere, so the
  token stayed in local storage for the life of the browser;
- a reload that always returned the player to whichever table was joined last,
  including one that had already ended.

Seats are now stored per match, capped at eight, and released when the match
ends, when the player leaves, and when the server says the match or the token
is gone. That last case is the one that mattered most: a seat the server has
rejected was retried on every reload, forever, because nothing ever forgot it.

### The pipeline naming its own problems

The lint gate failed with `ERR_MODULE_NOT_FOUND` from inside ESLint when the
root dependencies were not installed — a diagnostic that points at ESLint
rather than at the missing install. It now says which one it is.

`scripts/` is linted. It holds the certification pipeline — the thing that
produces the evidence for every other claim — and it was type-checked but never
linted. Adding the coverage found an unused import on its first run.

The repository CI job no longer installs five hundred packages to run two
scripts that import nothing but the standard library.

### The smaller ones

- A migration added late with a version below one already applied is now
  **refused**. The runner compared against the highest applied version, so it
  could not tell "already run" from "will never run" — and the second leaves
  the schema behind the code, with the server starting happily against a
  database it cannot write to. The runner now keeps the _set_ of applied
  versions.
- The `config` package has tests for the first time, including the security-
  relevant default that the environment is `development` unless it is set
  otherwise.
- `Broadcast` no longer takes a parameter it never used; `Bind` no longer
  contains a dead block.

## v0.2.10 — dependencies and modernisation (shipped)

A dependency update and a modernisation release. No gameplay change, no
protocol change, no API change.

### The toolchain

The question this release had to answer was whether ESLint and Prettier were
carrying their weight. The answer was no, and the evidence was:

|                          | ESLint + Prettier | oxlint + oxfmt             |
| ------------------------ | ----------------- | -------------------------- |
| Transitive packages      | 72                | 7                          |
| `.astro` linting         | never             | correct                    |
| Needs the TypeScript API | yes               | no                         |
| Reformat on adopting     | —                 | 2 of 48 files              |
| Config migration         | —                 | `oxfmt --migrate=prettier` |

**The decisive finding was not a performance number.** The ESLint configuration
matched only `**/*.{ts,tsx,mts}`, so ESLint reported every `.astro` file as
_"ignored because no matching configuration was supplied"_. The website — the
changelog, the game page, every component — was formatted by Prettier and
**never linted at all**. That is the gap this release closes.

Biome was evaluated and rejected: it handles `.astro` too, but its frontmatter
support produced twelve false positives on imports used only in a template, and
its formatter differed from Prettier's on seven files rather than two.

### TypeScript

TypeScript 7.0 ships no programmatic API until 7.1. `typescript-eslint` refuses
to load against it, and Astro's language server refuses it too — between them
they broke two of the seventeen gates.

The resolution does not need TypeScript 7 as a package at all:

- `bun check` is Bun's own typechecker. It reads the same tsconfig, reports
  the same error codes, needs no TypeScript package, and is about eighteen
  times faster. It was cross-validated against `tsc` on a strict-flag fixture
  and on the whole repository, and it works with `node_modules` deleted.
- `typescript` remains, aliased to `npm:@typescript/typescript6`, for exactly
  two things: `astro check`, which needs an API, and `tsc6` as the reference
  typechecker.
- **Both typecheckers must be clean.** A single typechecker is a single point
  of failure, and the two are independent implementations, so their agreement
  is the evidence that the fast one is equivalent here.

### The code

- **The history trim no longer allocates.** It copied the whole window into a
  fresh slice on every accepted action — about eleven kilobytes of garbage per
  action, on the path every player action takes. Copying the retained events
  down in place instead makes the trim about fifteen times faster and removes
  the allocation entirely. The retention window is still exactly 1024 events;
  only the allocation changed. Measured with a benchmark that is now part of the
  suite.
- **`apps/game/src/App.tsx` is now seven focused modules** in
  `apps/game/src/components/`, matching the convention `apps/web` already
  used. The server URL and the shared labels moved to modules of their own.
- **`server/internal/match/match.go` is now three files**: the runtime, the
  registry, and the rate limiter.
- **The Solid `ref` idiom is uniform.** Three sites used `ref={x}` while the
  fourth used `ref={(el) => (x = el)}`. Solid's compiler rewrites the first
  form into an assignment, so both work — but the linter cannot see it, and the
  implicit form is a trap for anyone who does not know the transform exists.
- **The CORS check matches the socket's.** Development mode relaxed the
  WebSocket origin check but not the HTTP one, so a developer running the game
  client against a local server without `HIGHJACK_ALLOWED_ORIGINS` got a bare
  CORS failure from the lobby while the socket connected happily.
- **Dead surface removed.** `Match.Resume` existed and was covered by the tests
  while production only ever called `ResumeFor`; the tests now cover the path
  that runs. `SavePlayer` was on the runtime's `Store` interface and called by
  nothing.

### A note on what was _not_ changed

The configuration validation is implemented twice — about 830 lines of Go and
340 of TypeScript — and that is deliberate rather than drift: the website
validates a configuration in the browser as the player edits it, and the server
validates it again as the authority. The two are covered by shared fixtures
that assert the same issue paths and the same issue kinds on both sides, so
leave it alone.

The Go and TypeScript codebases were already modern — no `interface{}`, no
classic `for i := 0; i < n; i++` loops, no legacy array patterns — and the
dependency graph was already minimal. There was nothing to fix there, and
saying so is more useful than inventing work.

## v0.2.11 — hygiene (shipped)

A dependency and hygiene release. No gameplay change, no protocol change, no
API change.

### The finding that mattered most: the release gate could not run in CI

`v0.2.10` was certified locally and **failed in CI**. The Frontend job's
Typecheck step died because CI pinned Bun 1.3.14, and `bun check` — the command
the typecheck gate runs — was added in Bun **1.4.3**.

The local certification said `CERTIFIED` and CI said red, on the same commit,
for the same gate. That is the same class of failure as the v0.2.1 stale-server
defect: a green local run that proved nothing about the release, because the
local machine and the release machine were running different tools.

Three things had to be true for it to happen, and all three are now closed:

- `packageManager` named `bun@1.3.14` — a Corepack pin for a runtime Corepack
  does not manage, naming a version that cannot run the project's own gate.
- CI pinned the same version independently.
- Nothing compared the pinned version against what the gates actually need.

The requirement is now stated once, in `engines.bun`, and `repo sanity` fails if
the workflow pins anything below it, if `engines.bun` is lowered, or if a
`packageManager` pin is added back. Each was verified by breaking it.

CI installs the newest Bun rather than a pin, so a regression in the newest
release surfaces immediately instead of at the next pin bump.

### The commands

The root scripts were doing the work of the workspace scripts by hand: a bash
`for` loop with `cd` into three directories, running `bunx vitest` where the
workspaces already had a `test` script. They now use `bun run --filter`, which
is the modern form, is not a shell loop, and picks up a new workspace without
editing the root.

| Script                                                     | What it does                                                           |
| ---------------------------------------------------------- | ---------------------------------------------------------------------- |
| `test`                                                     | every workspace's unit tests, then the Go suite with `-race`           |
| `test:go`                                                  | `go test -count=1 -race ./server/...` — the same assurance as the gate |
| `typecheck`                                                | `bun check`, the reference compiler, and `astro check` — all three     |
| `typecheck:ts` / `typecheck:reference` / `typecheck:astro` | each one on its own                                                    |
| `format` / `format:check`                                  | `oxfmt` in place, and `oxfmt --check`                                  |
| `lint`                                                     | `oxlint` and `go vet`                                                  |

The old `test` script ran `go test ./...` with no race detector and no
`-count=1`, so a local run was _weaker_ than the gate it was supposed to
preview. It is now the same.

The website's `test` script ran Vitest over no test files. It now has a
configuration that excludes `e2e/` — without it, `vitest run` collected the
Playwright specs and tried to execute them as unit tests, so a root `test` that
included the website reported a failure that had nothing to do with the code.

### Dependencies

- **`packageManager` removed.** It named a version that cannot run the gates.
- **`engines.bun: ">=1.4.3"` added** — the minimum, stated where the tools read
  it.
- **Two `overrides` removed as no-ops.** `js-yaml` and `seroval` were pinned
  for constraints that no longer exist: `seroval` was only needed because
  solid-js 1.9.15 asked for `~1.5.4`, and 1.9.17 asks for `~1.6.8`. The
  security floor those overrides provided is now held by the `audit:deps` gate,
  which is one mechanism rather than two.
- **`playwright-core` kept, as a range.** It is a no-op today, but it encodes a
  real invariant: `@playwright/test` and `@axe-core/playwright` must resolve
  the _same_ core, or the e2e suite has two incompatible `Page` types. A caret
  range enforces one version and still floats.
- **Workspace dependencies bumped**: pixi.js, solid-js, vitest, `@astrojs/solid-js`,
  `@astrojs/check`, `@axe-core/playwright`, `@playwright/test`, `@testing-library/user-event`,
  and jsdom across a major (26 → 30).

### Go

`go mod tidy -diff` was already empty and `go mod verify` already passed, so the
graph was already minimal — two direct requirements and five indirect, all at
their latest. Two things changed:

- The `go` directive was `1.26.8`, which is the patch of whoever last ran
  `tidy`, not a requirement the code has. It is now `1.26.0` — the minor
  version, which is what the directive is for.
- `go.sum` is unchanged: 28 lines for 17 modules, every module verified.

### The symlinks

The question worth answering plainly: **the symlinks are Bun's, not Go's.**
`go.mod` produces none — the Go module cache has zero symlinks in it. The 1190
symlinks in the tree are Bun's isolated install layout: `node_modules/<pkg>`
points into a content-addressed store at `node_modules/.bun/<pkg>@<version>+<hash>/`,
and `node_modules/.bin/*` point at the package binaries.

They are correct: **zero broken**, every one resolving to the exact version
the lockfile pins, every binary executable, and none of them tracked by git.
That layout is the point — it is how Bun stores one copy of each version and
links it, instead of npm's duplicated nested installs. The tree went from
seventy-odd transitive packages to seven partly because of it.

One real trap did surface: removing a direct dependency can leave a stale root
symlink behind, and the next install does not necessarily clear it. The gates
catch it, because they test for the tool's directory and a broken symlink
fails that test with a message that names the cause.

### Everything else

- `scripts/dev-server-bg.sh` was unreferenced — the Playwright harness stopped
  using it when it moved to the release build script. Removed.
- The Frontend CI job now sets up Go. Its browser gate builds and runs the
  real server, so it was depending on whatever Go the runner image happened to
  ship.
- The test database's teardown is its own step with `if: always()`. It used to
  be the last line of the test step, so a failing test left the container
  running.
- CI actions moved to their current majors: `setup-go@v7`, `setup-node@v7`,
  `upload-artifact@v7`.
- PostgreSQL 17 → 18 in the compose file, and Alpine 3.20 → 3.23 in the image.
  The database behaviour is proven: the whole persistence suite has run against
  PostgreSQL 18.6 throughout this cycle. The image could not be built here —
  Docker is not available in this environment — so CI is its first test.
- Five architecture and design documents still declared _"Status: v0.1.0
  Foundation"_ four releases after the board loop became playable. Corrected.
- The deployment README described a Redis deployment, a `v0.1` website with no
  server integration, and a build command that scraped the version out of
  `package.json` with `grep`. It now points at the `VERSION` file, states the
  one-instance constraint and why, and says plainly that
  `HIGHJACK_ALLOWED_ORIGINS` is not optional in production.
