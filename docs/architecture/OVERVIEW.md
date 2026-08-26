# HighJack Architecture Overview

> Status: v0.1.0 Foundation. This document describes what exists today.
> Future architecture is labeled explicitly as such.

## The core idea

**A HighJack match is a configurable ruleset executed by a deterministic
authoritative simulation.**

Everything in the repository is organized around that sentence:

- _Configurable ruleset_ → `GameConfig`, a first-class, validated,
  versioned domain object (`server/internal/game`, mirrored in
  `packages/protocol`).
- _Deterministic_ → all randomness flows through a seeded RNG abstraction;
  same inputs always produce the same outcome (`server/internal/game/rng.go`).
- _Authoritative_ → clients propose actions; only the server's engine
  mutates state (`Action → Validation → Transition → Events → New State`).

## Repository layout

```
highjack/
├── apps/
│   ├── web/          Astro website (static-first, SolidJS islands)
│   └── game/         Game client shell (SolidJS app surface + PixiJS stage)
├── packages/
│   ├── protocol/     Client/server wire contract (TypeScript)
│   └── ui/           Design system: tokens + shared Solid primitives
├── server/           Go 1.26 modular monolith
│   ├── cmd/highjack/ Server binary
│   ├── internal/     api, config, logging, game, protocol, realtime, persistence, version
│   └── migrations/   Explicit SQL migrations
├── assets/           Authored brand assets + provenance records
├── docs/            This documentation
├── infra/           Docker, Cloudflare, deployment configuration
├── scripts/         Validation / verification / certification pipeline
└── .github/         CI workflows
```

## System boundaries

```
                    Cloudflare (edge)
                         │
                 ┌───────┴────────┐
                 │                │
             Website          API / WebSocket
          (static pages)           │
                              Go server (modular monolith)
                                   │
                          ┌────────┴────────┐
                          │                 │
                     PostgreSQL        Redis (future — not deployed)
```

- Website and backend deploy independently.
- The server never serves the website; the website never embeds server state.

## Key decisions (and why)

| Decision                                    | Rationale                                                                                                                                                    |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Modular monolith, no microservices          | One product, one team-scale; boundaries enforced by Go package structure. Splitting later is mechanical if ever needed.                                      |
| Engine lives in Go, protocol mirrored in TS | The authoritative simulator must run where authority lives (server). Clients get a typed mirror validated by shared fixtures so drift is impossible to miss. |
| JSON on the wire                            | Human-debuggable, zero codegen, fast iteration. Replacement criteria documented in `docs/protocol/OVERVIEW.md`.                                              |
| Explicit SQL migrations                     | Schema truth is reviewable text, not ORM inference.                                                                                                          |
| Bun workspaces                              | Single lockfile/toolchain for all TypeScript; fast installs; native test runner available.                                                                   |
| SolidJS islands in an Astro site            | Marketing/content pages stay static and crawlable; interactivity ships only where it earns its JavaScript.                                                   |

## What does NOT exist yet

Board gameplay, trading, auctions, gambling tables, minigames, events,
matchmaking, accounts/authentication, chat, replays-as-a-feature. The
architectural seams for these exist (engine rulesets, protocol action/event
envelopes, persistence schema), but nothing pretends they are implemented.

See [GAME_ENGINE.md](./GAME_ENGINE.md), [BACKEND.md](./BACKEND.md),
[FRONTENDS.md](./FRONTENDS.md) for boundary-level details.
