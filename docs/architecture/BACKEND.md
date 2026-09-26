# Backend Architecture

> Status: v0.2.0 Board Loop. The HTTP surface, realtime transport,
> configuration, logging, persistence, and the authoritative match runtime
> exist and are tested. Private multiplayer matches are playable end to end.
> Account authentication is still not implemented.

## Shape

A **modular monolith** in Go 1.26, rooted at `server/`:

```
server/
├── cmd/highjack/          binary: wiring + lifecycle (main.go)
└── internal/
    ├── api/               HTTP endpoints + middleware chain
    ├── config/            environment loading + validation
    ├── logging/           structured slog setup
    ├── protocol/          Go mirror of @highjack/protocol (wire contract)
    ├── game/              authoritative engine boundary (see GAME_ENGINE.md)
    ├── match/             live match runtime: registry, dispatch, broadcast
    ├── realtime/          WebSocket transport + session/token binding
    ├── persistence/       PostgreSQL pool + SQL migration runner
    └── version/           build metadata (ldflags-injected)
```

Dependencies flow one way: `cmd → api → {realtime, game, …} → stdlib`.
The `game` package imports nothing internal; `protocol` imports nothing
internal.

## HTTP surface

| Route                        | Purpose                                                                                                                                         |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /health`                | Liveness — process is up. Never inspects dependencies.                                                                                          |
| `GET /ready`                 | Readiness — 200 when all configured dependencies pass their checks, 503 with a stable reason code otherwise. Internal error detail never leaks. |
| `GET /version`               | Build info: name, version, commit, Go version, protocol/schema versions.                                                                        |
| `POST /matches`              | Create a private match. Optional config is validated; the caller never supplies a seed. Returns id + player token.                              |
| `POST /matches/{id}/players` | Claim a seat. Returns the player id and a reconnect token (shown exactly once).                                                                 |
| `GET /matches/{id}`          | Authoritative snapshot for a match (lobby join screen and resync).                                                                              |
| `GET /ws`                    | WebSocket seam (below).                                                                                                                         |

### Middleware chain (in order)

```
recoverPanics → withRequestID → securityHeaders → accessLog → router
```

- **recoverPanics**: panics become 500s; stack traces stay in logs.
- **withRequestID**: mints a random id (or honors a sane caller-supplied
  `X-Request-Id`, rejecting injection attempts), attaches it to the request
  context logger and echoes the header.
- **securityHeaders**: `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Cache-Control: no-store`.
- **accessLog**: one structured JSON line per request.

### Server timeouts & lifecycle

`ReadHeaderTimeout` 5s · `ReadTimeout` 15s · `WriteTimeout` 15s ·
`IdleTimeout` 120s. The WebSocket handler clears per-connection deadlines
via `http.NewResponseController` so long-lived streams survive.
On SIGINT/SIGTERM the server drains within `ShutdownTimeout` (10s) and
exits non-zero only on real failures.

## Configuration

Environment-driven (`HIGHJACK_ENV`, `HIGHJACK_ADDR`,
`HIGHJACK_DATABASE_URL`, `HIGHJACK_LOG_LEVEL`), loaded once at startup into
an immutable struct that main wires explicitly into components. No global
mutable state, no hidden reads. Invalid configuration refuses to boot.

## Realtime seam

```
WebSocket → Connection → Message Decoder → match runtime → Engine → broadcast
```

1. Client must send `hello` first; anything else gets
   `error{code:"malformed_message"}`.
2. Wrong protocol major version → `unsupported_version`.
3. `ping` → `pong{nonce}`.
4. `hello` with `matchId` + `token` binds the connection to a player;
   with `resumeFromTick` it also replays retained events. The shipped
   client does not yet send a cursor (audit CLT-9), so catch-up is
   currently only reachable by a client that supplies one.

The actor for an action is always the connection binding. The decoder
lives in `internal/protocol` and is fixture-tested against
`packages/protocol/fixtures/messages/`. Integration tests drive two real
WebSocket clients through a real match.

## Match runtime (v0.2)

`internal/match` owns live matches. The engine stays pure; this package
serializes access around it.

```
Match ── engine (pure)     Apply(state, actor, action)
      ── mutex             one transition at a time per match
      ── state             authoritative GameState
      ── history ring      1024 events, drives catch-up
      ── sessions          session ↔ player bindings
```

- **Registry** holds matches by id. Creation mints an unguessable
  `m_`-prefixed id and a 256-bit root seed.
- **Dispatch order** is fixed: validate session → validate envelope →
  resolve actor → validate sequence → serialize → `Engine.Apply` → persist
  → publish events → ack. Nothing is broadcast that is not durable.
- **Locking**: the per-match mutex is released before any network write, so
  one slow client cannot stall the match. It _is_ still held across the
  database commit, which is a known limitation tracked in the audit as
  PRS-3 and fixed in v0.2.2.
- **Broadcast failure** never blocks the game loop: a dead sink is
  dropped, and the client reconnects via snapshot. A repeated action sequence
  is answered with the ack only, never re-published, so a retry cannot
  double-apply an economic change on a peer.
- **Restart policy**: leftover matches are moved to `interrupted` at boot.
  Migrations are applied at boot, and readiness checks the schema as well as
  connectivity, so a server cannot report itself ready against a database it
  cannot write to. v0.2 does not restore live matches across process restarts;
  see [GAME_DESIGN.md](../game/GAME_DESIGN.md).
- **Bounded resources**: the live-match registry and the per-session rate
  limiter are both bounded and swept, so an unauthenticated caller cannot grow
  process memory without limit.

## CORS and origins (v0.2)

The lobby and the WebSocket share one explicit allow-list,
`HIGHJACK_ALLOWED_ORIGINS` (comma-separated `scheme://host`; empty means
same-origin only). Configured origins are echoed exactly with
`Vary: Origin`; preflight is answered `204`. The WebSocket handshake
applies the same list. This replaced the previous wildcard origin, so a
browser on an unlisted origin can call neither the lobby nor the socket.

## Observability

Structured JSON logs on stdout with stable fields: `time`, `level`,
`msg`, `service`, `version`, plus `requestId` on request-scoped lines.

Path for the future: metrics/tracing should hook into the middleware chain
and the engine's event stream; no Prometheus/OpenTelemetry stack is
deployed at this stage by design. The API's CORS behavior is an explicit
allow-list (see **CORS and origins**), not an open policy.

## Authentication boundary

**Accounts are not implemented** and nothing fakes them. What v0.2 does
have is per-match identity:

- Claiming a seat mints a 256-bit reconnect token, returned exactly once.
- Only its sha256 is stored, so a database leak does not yield usable
  tokens. Tokens are never logged.
- A token authenticates **within one match only**, enforced by the
  in-memory binding table and covered by an integration test. The durable
  `game_players.token_hash` column and its unique index are written but not
  read at runtime (audit A-7).
- Connection → player binding happens at `hello`; after that the server
  ignores any client-supplied identity entirely.

The one deliberate gap: a token is a bearer credential. There is no rate
limit on `POST /matches/{id}/players` beyond the global HTTP limits, so
brute-forcing is impractical (256-bit space) but unbounded. A real auth
package would own seat creation, which is a clean seam: the lobby API
already mints every token in one place.

## Persistence

See migrations in `server/migrations/` and the runner in
`internal/persistence`. Details and developer workflow:
[SETUP.md](../development/SETUP.md).
