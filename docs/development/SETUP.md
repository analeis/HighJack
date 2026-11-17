# Development Setup

## Requirements

| Tool                  | Version    | Notes                                  |
| --------------------- | ---------- | -------------------------------------- |
| [Bun](https://bun.sh) | ≥ 1.4.3    | Workspaces, tests, builds, scripts     |
| [Go](https://go.dev)  | 1.26.x     | Backend                                |
| Docker                | any recent | Only for the optional local PostgreSQL |

Node is not required directly (Bun drives everything); CI installs the newest
active Node LTS for Playwright's driver, which fetches a browser on first e2e
run.

### Why Bun 1.4.3 and not "whatever is installed"

The typecheck gate runs `bun check`, which was added in Bun 1.4.3. The
repository used to pin `bun@1.3.14` through the `packageManager` field, and
CI pinned the same version — a version that cannot run the project's own gate.
The gate failed in CI with "command not found" while the identical gate passed
locally, which is the worst possible failure: a green local run that says
nothing about the release.

So there is no `packageManager` field now. The requirement is stated as a
minimum in `engines.bun`, and CI installs the newest Bun, so a regression in the
newest release surfaces immediately rather than at the next pin bump.

### Floating toolchains in CI

CI floats its tool versions but keeps `uses:` refs on major tags:

| Input          | Value    | Why                                                                   |
| -------------- | -------- | --------------------------------------------------------------------- |
| `bun-version`  | `latest` | Needs `bun check`; an old pin once failed the typecheck gate in CI    |
| `go-version`   | `stable` | A pinned patch drifted into advisory GO-2026-6859 and broke a release |
| `node-version` | `lts/*`  | New enough for Playwright without tracking Node's bleeding edge       |

A GitHub Action has no `latest` tag — its major tag (`@v7`, `@v2`) _is_ the
floating ref, updated to the latest release in that major. Tracking `@main`
would run unreleased action code, which is the opposite of the reliability this
project needs after two toolchain-drift releases, so the majors stay pinned.

## Bootstrap

```sh
git clone https://github.com/analeis/highjack && cd highjack
bun install          # installs all workspace dependencies
go mod download      # backend module cache
```

If `astro check` fails with an `emnapi` native-module error after a dependency
change, the lockfile is stale rather than the versions wrong. Remove `bun.lock`
and `node_modules`, then `bun install` again to resolve from scratch.

Verify your toolchain:

```sh
go version   # expect go1.26.x
bun --version
```

## Run the pieces

### Website (`apps/web`)

```sh
bun run dev          # http://localhost:4321
```

Production build + local preview:

```sh
bun run build:web
bun run --filter='@highjack/web' preview
```

### Game shell (`apps/game`)

```sh
bun run dev:game     # http://localhost:5173 — the playable board loop
```

### Backend

No database needed for basic development:

```sh
go run ./server/cmd/highjack        # http://localhost:8080/health
curl localhost:8080/version
```

With PostgreSQL (persistence enabled):

```sh
docker compose -f infra/docker/docker-compose.dev.yml up -d
export HIGHJACK_DATABASE_URL="postgres://highjack:highjack@localhost:5432/highjack?sslmode=disable"
go run ./server/cmd/highjack         # migrations apply automatically at boot
curl localhost:8080/ready            # reflects database connectivity
```

Environment variables (all optional):

| Variable                     | Default       | Purpose                                                              |
| ---------------------------- | ------------- | -------------------------------------------------------------------- |
| `HIGHJACK_ENV`               | `development` | `development` \| `production` \| `test`                              |
| `HIGHJACK_ADDR`              | `:8080`       | Listen address                                                       |
| `HIGHJACK_DATABASE_URL`      | _(empty)_     | Postgres DSN; empty disables persistence                             |
| `HIGHJACK_LOG_LEVEL`         | `info`        | `debug` \| `info` \| `warn` \| `error`                               |
| `HIGHJACK_MATCH_IDLE_TTL`    | `1h`          | How long a match with no connected client is kept before eviction    |
| `HIGHJACK_LOBBY_RATE_LIMIT`  | `60`          | Lobby requests per client address per window; `0` disables the limit |
| `HIGHJACK_LOBBY_RATE_WINDOW` | `1m`          | The sliding window the lobby rate limit applies over                 |

The release version lives in the repository's `VERSION` file, not in an
environment variable: the workspace manifests, the game client's build, the
server binary's `/version` endpoint, and the release tag are all checked
against it.

## Migrations

Migrations are explicit SQL under `server/migrations/` named
`NNNN_description.sql`. They are applied in order by the server at boot
(only when a database is configured) and tracked in `schema_migrations`.
To create one:

1. Add `server/migrations/0002_my_change.sql`
2. Write plain SQL (it runs inside a transaction together with its bookkeeping row)
3. Start the server; it applies pending migrations idempotently

## Full validation

```sh
bun run check:all    # lint + typecheck + format:check — "is this tree clean?"
bun run test         # every workspace's unit tests, then the Go suite with -race
bun run typecheck    # bun check, the reference compiler, and astro check
bun run lint         # oxlint and go vet
bun run format       # oxfmt, in place
bun run format:check # oxfmt, without writing
bun run validate     # formatting, lint, typecheck, unit tests, builds, asset & repo sanity
bun run certify      # validate + browser verification + release classification
```

### The script graph

The convenience scripts and the certification gates are deliberately separate:

- `test`, `build` and `check` are developer conveniences. `test` runs each
  workspace's own `test` through one `--filter` expression; `build` builds both
  apps and the server through one expression; `check:all` folds the three static
  checks into one command. (It is not named `check`, because Bun has a built-in
  `bun check` typechecker that `typecheck:ts` relies on — a script with that name
  shadows the built-in.) Adding a workspace needs no edit to these.
- The gates in `scripts/lib/validation-gates.ts` and `verification-gates.ts`
  are the single definition of what certification checks. They call the
  workspace-local scripts directly (for example `bun run build` in `apps/web`),
  not the root conveniences, so a convenience edit cannot weaken a gate.
- `validate` and `certify` intentionally share one gate-list definition. They
  import the same arrays; `certify` simply adds browser verification and the
  release classification. This is what makes it impossible for the two
  pipelines to drift apart. CI invokes the same gates by name via
  `bun run gate <name>`.

See [VALIDATION.md](./VALIDATION.md) and [TESTING.md](./TESTING.md).

## Troubleshooting

- **Playwright browser missing** → `cd apps/web && bunx playwright install chromium`
- **Port already in use** → set `HIGHJACK_ADDR=127.0.0.1:PORT` / pass Astro's `--port`
- **Database tests skipped** → expected unless `HIGHJACK_TEST_DATABASE_URL` is set (see TESTING.md)
