# Testing Strategy

> Principle: tests exist from the first commit, and no test suite should
> require production infrastructure to run. Integration suites opt in
> explicitly.

## Map

| Layer                    | Tool                                      | Location                                           | Runs in `go test ./...` / vitest?                   |
| ------------------------ | ----------------------------------------- | -------------------------------------------------- | --------------------------------------------------- |
| Engine domain (Go)       | `go test`                                 | `server/internal/game/*_test.go`                   | yes (hermetic)                                      |
| HTTP endpoints (Go)      | `httptest`                                | `server/internal/api/api_test.go`                  | yes                                                 |
| WebSocket seam (Go)      | real dial over loopback                   | `server/internal/realtime/handler_test.go`         | yes                                                 |
| Protocol decode (Go)     | fixtures                                  | `server/internal/realtime/protocol_compat_test.go` | yes                                                 |
| Migrations parsing (Go)  | `go test`                                 | `server/internal/persistence/migrate_test.go`      | yes                                                 |
| Database round-trip (Go) | pgx against live PG                       | `server/internal/persistence/integration_test.go`  | **skipped unless** `HIGHJACK_TEST_DATABASE_URL` set |
| Protocol + config (TS)   | Vitest                                    | `packages/protocol/test`                           | yes                                                 |
| UI primitives (TS)       | Vitest + @solidjs/testing-library + jsdom | `packages/ui/test`                                 | yes                                                 |
| Shell store (TS)         | Vitest                                    | `apps/game/test`                                   | yes                                                 |
| Website e2e + a11y       | Playwright + axe                          | `apps/web/e2e`                                     | via `bun run test:e2e` / certify only               |

## Frontend unit conventions

- Solid components are tested through their accessibility contract:
  roles, aria states, and keyboard behavior — not implementation details.
- jsdom environment; `@solidjs/testing-library` with automatic cleanup
  (`test/setup.ts`).
- No snapshot tests of markup; assert what a user or screen reader gets.

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

- **Protocol compatibility**: shared fixtures under
  `packages/protocol/fixtures/` are consumed by both Go and TS suites.
  Changing a fixture without changing both implementations fails CI.

## Running

```sh
bun run test         # all vitest suites (protocol, ui, game)
go test ./...        # all hermetic Go suites
bun run --filter='@highjack/web' test:e2e    # browser verification
```
