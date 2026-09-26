# Testing Strategy

> Principle: tests exist from the first commit, and no test suite should
> require production infrastructure to run. Integration suites opt in
> explicitly.

## Map

| Layer                    | Tool                                      | Location                                                | Runs in `go test ./...` / vitest?                   |
| ------------------------ | ----------------------------------------- | ------------------------------------------------------- | --------------------------------------------------- |
| Engine domain (Go)       | `go test`                                 | `server/internal/game/*_test.go`                        | yes (hermetic)                                      |
| HTTP endpoints (Go)      | `httptest`                                | `server/internal/api/api_test.go`                       | yes                                                 |
| WebSocket seam (Go)      | real dial over loopback                   | `server/internal/realtime/handler_test.go`              | yes                                                 |
| Multiplayer match (Go)   | two real WebSocket clients                | `server/internal/realtime/multiplayer_test.go`          | yes                                                 |
| Match runtime (Go)       | `go test`                                 | `server/internal/match/match_test.go`                   | yes                                                 |
| Protocol decode (Go)     | fixtures                                  | `server/internal/realtime/protocol_compat_test.go`      | yes                                                 |
| Migrations parsing (Go)  | `go test`                                 | `server/internal/persistence/migrate_test.go`           | yes                                                 |
| Database round-trip (Go) | pgx against live PG                       | `server/internal/persistence/integration_test.go`       | **skipped unless** `HIGHJACK_TEST_DATABASE_URL` set |
| Match persistence (Go)   | pgx against live PG                       | `server/internal/persistence/match_integration_test.go` | **skipped unless** `HIGHJACK_TEST_DATABASE_URL` set |
| Protocol + config (TS)   | Vitest                                    | `packages/protocol/test`                                | yes                                                 |
| UI primitives (TS)       | Vitest + @solidjs/testing-library + jsdom | `packages/ui/test`                                      | yes                                                 |
| Game client store + net  | Vitest                                    | `apps/game/test`                                        | yes                                                 |
| Website e2e + a11y       | Playwright + axe                          | `apps/web/e2e`                                          | via `bun run test:e2e` / certify only               |
| Game UI e2e              | Playwright against the **real Go server** | `apps/web/e2e/game.spec.ts`                             | via `bun run test:e2e` / certify only               |

## Frontend unit conventions

- Components in `packages/ui` are tested through their accessibility contract:
  roles, aria states, and keyboard behavior — not implementation details.
  They run under jsdom with `@solidjs/testing-library` and automatic cleanup
  (`packages/ui/test/setup.ts`).
- `apps/game` runs under the **node** environment, not jsdom, and has neither
  `@solidjs/testing-library` nor a setup file. Its tests cover the store and the
  network layer. **The `App.tsx` UI has no component tests**; its behaviour is
  covered by the browser suite instead. Do not assume otherwise when adding
  coverage.
- No snapshot tests of markup; assert what a user or screen reader gets.

## CI and the pipeline must agree

`repo sanity` fails if a gate defined in `scripts/lib/validation-gates.ts` is not
also enforced by `.github/workflows/ci.yml`. The comparison is on substance, not
spelling: it normalises away output paths and shell control operators, because
certification builds to a temp directory while CI builds to `./bin`, and a gate may
report success with `&& echo` while CI prefers `|| { ...; exit 1; }`.

The check exists because the two drifted invisibly. CI's database step once carried
a `-run` filter that excluded the JSONB round-trip and the interrupted-match policy
tests while the step still reported success.

The backend job runs the Go toolchain directly — `gofmt`, `go vet`, `govulncheck`,
`go test -race` and `go build` — with **no JavaScript runtime installed at all**.
Invoking those through `bun scripts/gate.ts`, as the job previously did, made the Go
build depend on a runtime the job never installed; it worked only because that script
has no external imports. If you add a Go gate, add it to the workflow and let the
sanity check confirm it.

## CI and the pipeline must agree

`repo sanity` fails if a gate defined in `scripts/lib/validation-gates.ts` is not
also enforced by `.github/workflows/ci.yml`. The comparison is on substance, not
spelling: it normalises away output paths and shell control operators, because
certification builds to a temp directory while CI builds to `./bin`, and a gate may
report success with `&& echo` while CI prefers `|| { ...; exit 1; }`.

The check exists because the two drifted invisibly. CI's database step once carried
a `-run` filter that excluded the JSONB round-trip and the interrupted-match policy
tests while the step still reported success.

The backend job runs the Go toolchain directly — `gofmt`, `go vet`, `govulncheck`,
`go test -race` and `go build` — with **no JavaScript runtime installed at all**.
Invoking those through `bun scripts/gate.ts`, as the job previously did, made the Go
build depend on a runtime the job never installed; it worked only because that script
happens to have no external imports. If you add a Go gate, add it to the workflow and
let the sanity check confirm it.

## Browser harness

The Playwright harness never reuses an existing **game server** or **game
preview**: both are started fresh, because a leftover server from an earlier build
answers `/health` happily while running entirely different rules. That is not
hypothetical — a stale v0.2.0 binary was serving the game suite for two
certified releases, and every game-path assertion was verifying pre-audit code.
The website preview is still reused locally; it is static content and carries no
assertions about backend behaviour.

## Backend conventions

- Table-driven tests; every domain error path covered.
- Determinism proof: run the same seeded scenario twice, compare
  serialized state and event logs byte-for-byte
  (`TestDeterministicSimulationSameInputSameOutput`).
- Invariants: invalid action leaves state untouched; tick advances exactly
  once per applied action; phase transitions are guarded.
- Graceful shutdown is tested: `Shutdown` completes well within its window
  and the listener returns.

## Engine testing philosophy

Every engine test follows the shape:

```
Given:  input state + deterministic seed
When:   one action
Then:   exact new state + exact events
```

Example from the current lifecycle:

```text
Given: seed X, fresh lobby, config starting_money = 1500
When:  player joins as "Ace"
Then:  state has one player at seat 0 holding 1500, host flag set,
       one player_joined event at tick 1
```

Roadmap for future suites (in priority order):

1. **Table-driven scenario files** — recorded `(seed, actions) → events`
   cases reviewed like specs.
2. **Property-based tests** — randomized-but-seeded action sequences
   checked against invariants (money conservation, ownership uniqueness,
   phase legality).
3. **Replay tests** — event logs re-applied to genesis state must
   reproduce final state hashes.
4. **Invariant fuzzing** — long random simulations asserting the invariant
   list in GAME_ENGINE.md never breaks.

## Integration conventions

- **HTTP/WebSocket**: covered hermetically via `httptest` — always on.
- **PostgreSQL**: gated behind `HIGHJACK_TEST_DATABASE_URL`. Provide an
  isolated database locally with:

  ```sh
  docker compose -f infra/docker/docker-compose.dev.yml up -d
  export HIGHJACK_TEST_DATABASE_URL="postgres://highjack:highjack@localhost:5432/highjack_test?sslmode=disable"
  go test ./server/...
  ```

  Tests skip loudly when unset; they never fail CI by being skipped.

  Without Docker, a throwaway local cluster works just as well:

  ```sh
  export PGDATA=/tmp/highjack-pg SOCKDIR=/tmp/highjack-sock
  mkdir -p "$SOCKDIR"
  initdb -D "$PGDATA" -U highjack --auth=trust
  pg_ctl -D "$PGDATA" -o "-p 5433 -c unix_socket_directories=$SOCKDIR" -l /tmp/pg.log start
  createdb -h 127.0.0.1 -p 5433 -U highjack highjack_test
  export HIGHJACK_TEST_DATABASE_URL="postgres://highjack@127.0.0.1:5433/highjack_test?sslmode=disable"
  go test ./server/...
  ```

  `pg_ctl` writes its lock file under the socket directory, so on machines
  where `/var/run/postgresql` is not writable the custom
  `unix_socket_directories` is required. CI uses the same port (5433) for
  the same reason.

  These suites are load-bearing, not decorative: they are what caught the
  pre-match `turn_phase` constraint violation and the missing
  interrupted-status handling that unit tests with fakes cannot see.

- **Protocol compatibility**: shared fixtures under
  `packages/protocol/fixtures/` are consumed by both Go and TS suites.
  Changing a fixture without changing both implementations fails CI.

## Running

```sh
bun run test         # all vitest suites (protocol, ui, game)
go test ./...        # all hermetic Go suites
bun run --filter='@highjack/web' test:e2e    # browser verification
```
