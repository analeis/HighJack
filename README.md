# HighJack

**A browser-first multiplayer board/economy game where every match is a configurable ruleset executed by a deterministic authoritative simulation.**

HighJack combines property/economic gameplay with optional systems: trading, auctions, gambling tables, minigames, treasure cards, and global events; all switchable per-match through a first-class game configuration.

> **Status: v0.2.7 — playable board loop.** The backend, protocol, and tooling are
> the production foundation; the board loop is now a real, playable multiplayer
> game. A match is a configurable ruleset executed by a deterministic
> authoritative simulation: the server owns every dice roll, balance, and
> ownership change, and the client may only render and request. See the
> [changelog](apps/web/src/pages/changelog.astro) for what is in each release,
> and [docs/audit/V0_2_5_PATCHWORK.md](docs/audit/V0_2_5_PATCHWORK.md) for the
> current post-review work.
>
> The current release version is in [`VERSION`](VERSION). It is the single source
> of truth: the workspace manifests, the game client's build, the server
> binary's `/version` endpoint, and the release tag are all checked against it,
> so they cannot drift apart.

## What is inside

| Path                | Purpose                                                                                 |
| ------------------- | --------------------------------------------------------------------------------------- |
| `apps/web`          | Product website (Astro + SolidJS + Tailwind)                                            |
| `apps/game`         | Game client shell (SolidJS application UI + PixiJS render surface)                      |
| `packages/protocol` | Shared client/server protocol contract                                                  |
| `packages/ui`       | Design system: tokens + shared UI primitives                                            |
| `server/`           | Go 1.26 modular-monolith backend (health/readiness/version, realtime seam, persistence) |
| `assets/`           | Authored brand/board/card source assets                                                 |
| `docs/`             | Architecture, design, protocol, and development documentation                           |
| `infra/`            | Docker, Cloudflare, and deployment configuration                                        |
| `scripts/`          | Validation / verification / certification pipeline                                      |

## Quick start

Requirements: [Bun](https://bun.sh) ≥ 1.3, [Go](https://go.dev) 1.26, Docker (only for the optional local database).

```sh
bun install          # install workspace dependencies
go test ./...        # run backend tests
bun run test         # run frontend unit tests
bun run dev          # run the website
```

`bun install` must be run **at the repository root**. The lint and format gates
read the root's `eslint.config.js` and `prettier` configuration, and they fail
without it — in a way that points at ESLint rather than at the missing install.

Backend:

```sh
go run ./server/cmd/highjack
# → http://localhost:8080/health
```

Full setup instructions (including PostgreSQL via Docker Compose): [`docs/development/SETUP.md`](docs/development/SETUP.md).

## Validation & certification

```sh
bun run validate     # formatting, lint, typecheck, tests, builds, asset & repo sanity
bun run certify      # validation + browser verification → release classification
```

See [`docs/development/CERTIFICATION.md`](docs/development/CERTIFICATION.md).

## Documentation

- [Architecture overview](docs/architecture/OVERVIEW.md)
- [Game engine model](docs/architecture/GAME_ENGINE.md)
- [Protocol](docs/protocol/OVERVIEW.md)
- [Design system & art direction](docs/design/DESIGN_SYSTEM.md)

## License

[MIT](LICENSE)
