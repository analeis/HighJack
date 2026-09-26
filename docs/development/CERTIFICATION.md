# Build Certification

HighJack classifies builds through a fixed pipeline. A build may only be
called **CERTIFIED** when every mandatory gate has passed on exactly the
tree being certified. The result must be reproducible: re-running
certification on the same commit yields the same classification.

```
DEVELOPMENT ──▶ VALIDATED ──▶ VERIFIED ──▶ CERTIFIED ──▶ RELEASED
   (work in      (bun run      (browser     (all gates     (tagged,
    progress)     validate)     suite)       recorded)     deployed)
```

## Gate matrix

| Category             | Gate                                                                                                         | Mandatory                             | Tool                                  |
| -------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------------------------- | ------------------------------------- |
| Formatting           | no formatting diffs                                                                                          | ✔                                     | prettier --check, gofmt -l            |
| Static analysis      | lint + vet clean                                                                                             | ✔                                     | eslint, go vet                        |
| Types                | typecheck clean                                                                                              | ✔                                     | tsc, astro check                      |
| Unit tests           | all suites green                                                                                             | ✔                                     | vitest, go test                       |
| Integration          | hermetic HTTP/WS/protocol suites green                                                                       | ✔                                     | go test                               |
| Database integration | migration round-trip green                                                                                   | ○ optional until a deployment uses PG | go test (env-gated)                   |
| Builds               | web · game · server binaries build                                                                           | ✔                                     | astro/vite/go build                   |
| Assets               | naming, size ceilings, provenance manifest                                                                   | ✔                                     | scripts/check-assets.ts               |
| Repository sanity    | structure, secrets scan, size caps, doc links                                                                | ✔                                     | scripts/repo-sanity.ts                |
| Browser verification | routes render, navigation, no console errors, responsive smoke                                               | ✔                                     | Playwright                            |
| Accessibility        | axe scans pass on key pages; keyboard-critical behavior tested                                               | ✔                                     | @axe-core/playwright + unit contracts |
| Security sanity      | no committed secrets; security headers present; protocol input boundaries enforced                           | ✔                                     | repo-sanity + api/ws tests            |
| Dependency sanity    | lockfile committed; `bun install` reproducible; no known-vulnerable deps; no known-vulnerable Go stdlib/deps | ✔                                     | `bun audit`, `govulncheck`            |
| Concurrency          | server suite under the race detector, uncached                                                               | ✔                                     | `go test -race -count=1`              |
| Deployment artifacts | Docker image builds; Cloudflare config valid                                                                 | ✔ at RELEASED step                    | infra checks                          |

## Certification levels

| Level       | Meaning                                                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| DEVELOPMENT | Work-in-progress tree; no guarantees.                                                                                          |
| VALIDATED   | `bun run validate` fully green — structurally and technically sound.                                                           |
| VERIFIED    | VALIDATED + browser/accessibility verification green — behaves as intended for users.                                          |
| CERTIFIED   | VERIFIED + security/dependency/deployment-artifact gates reviewed and recorded in the certification report. Release candidate. |
| RELEASED    | A CERTIFIED build that is tagged and deployed; deployment target and artifact digests recorded here or in release notes.       |

## What certification asserts about the tree

The report identifies the **tree**, not merely the last commit:

| Field          | Meaning                                                          |
| -------------- | ---------------------------------------------------------------- |
| `commit`       | `git rev-parse HEAD` at certification time                       |
| `tree`         | `git write-tree` — the content that was actually tested          |
| `clean`        | whether the working tree matched `commit` exactly                |
| `dirtyPaths`   | which files differed, when it did not                            |
| `skippedGates` | mandatory gates that did not run, and optional ones that did not |

`bun run certify` **refuses to certify a dirty tree** and exits non-zero. A
report that named only `commit` could attest to a commit whose content was
never tested, which is exactly how v0.2.0 shipped with a report naming an
earlier commit than the tag it was released under.

A skipped **mandatory** gate fails certification. Only genuinely
environment-gated gates (currently just the PostgreSQL suite when no
`HIGHJACK_TEST_DATABASE_URL` is configured) are optional, and they are
recorded as `skipped`, never as `passed`.

## Single definition of a gate

`scripts/lib/validation-gates.ts` and `scripts/lib/verification-gates.ts` are
the only definitions of what a gate does. CI invokes them by name with
`bun scripts/gate.ts <name>` rather than restating the commands, because a
second copy of the gate list is a second thing to forget to update — which is
how the CI database step came to filter out the very tests it was meant to run.

## Procedure

1. Clean checkout of the exact commit under consideration.
2. `bun install && go mod download`.
3. `bun run certify` → must exit 0, and must report `CERTIFIED`.
4. Review `verification/report.json` (gate list, timestamps, commit, tree,
   `clean`, `dirtyPaths`, `skippedGates`).
5. Security & dependency sanity review (secrets scan output, audit run).
6. Record the classification (release notes / tag message) with:
   - commit SHA
   - gate summary from the report
   - any _optional_ gates skipped and why

## Reproducibility rules

- Certification never depends on machine-local state: no global caches are
  consulted by gates; Playwright downloads its own pinned browser.
- Optional gates that were skipped are listed as skipped, not "passed".
- If any gate is flaky, it gets fixed or explicitly demoted to optional —
  a known-flaky mandatory gate invalidates certification.

## Current status

- v0.1.0 Foundation targets **CERTIFIED** on the foundation tree with the
  database-integration gate exercised locally (documented in TESTING.md)
  and recorded as optional in CI (no persistent PG service yet).
