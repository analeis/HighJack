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

| ID    | Finding                                                          | Severity        | Disposition                     |
| ----- | ---------------------------------------------------------------- | --------------- | ------------------------------- |
| CRT-1 | Retention window split a transition                              | HIGH            | **Fixed in v0.2.5**             |
| CRT-2 | A full registry reported as 500                                  | HIGH            | **Fixed in v0.2.5**             |
| CRT-3 | A chunked request's config was ignored                           | LOW             | **Fixed in v0.2.5**             |
| TST-1 | Two tests failed at random                                       | HIGH (evidence) | **Fixed in v0.2.5**             |
| CAP-1 | The registry never evicts; the lobby is unrated                  | HIGH            | **v0.2.6**                      |
| TRC-1 | No build path produces an identifiable binary                    | MEDIUM          | **Fixed in v0.2.7**             |
| TRC-2 | README, manifests and client UI name the wrong version           | MEDIUM          | **Fixed in v0.2.7**             |
| PRO-1 | The TS protocol requires a `seed` the server strips              | MEDIUM          | **v0.2.7**                      |
| PRO-2 | Parity tests compare vocabulary, not field shapes                | MEDIUM          | **v0.2.7**                      |
| TOK-1 | The stored seat is never cleared and is not per-match            | MEDIUM          | **v0.2.8**                      |
| CER-1 | The ESLint gate needs an unprovisioned root install              | MEDIUM          | **v0.2.8**                      |
| CER-2 | `scripts/` is type-checked but never linted                      | LOW             | **v0.2.8**                      |
| LOW-1 | `Broadcast`'s `exceptSeq` is unused                              | LOW             | **v0.2.8**                      |
| LOW-2 | Dead block in `Bind`                                             | LOW             | **v0.2.8**                      |
| LOW-3 | The `config` package has no tests                                | LOW             | **v0.2.8**                      |
| LOW-4 | The migration runner ignores a late-added lower version          | LOW             | **v0.2.8**                      |
| LOW-5 | The `repository` CI job installs 518 packages to run two scripts | LOW             | **v0.2.8**                      |
| RSK-1 | Multi-instance correctness                                       | —               | **Open — deployment decision**  |
| PRS-3 | The match mutex is held across the commit                        | —               | **Open — concurrency decision** |

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
