# Backend Architecture

> Status: v0.1.0 Foundation. The HTTP surface, realtime seam, configuration,
> logging, and persistence infrastructure exist and are tested. Lobby/game
> orchestration, authentication, and full multiplayer are future work.

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
    ├── realtime/          WebSocket transport seam
    ├── persistence/       PostgreSQL pool + SQL migration runner
    └── version/           build metadata (ldflags-injected)
```

Dependencies flow one way: `cmd → api → {realtime, game, …} → stdlib`.
The `game` package imports nothing internal; `protocol` imports nothing
internal.

## HTTP surface

| Route          | Purpose                                                                                                                                         |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /health`  | Liveness — process is up. Never inspects dependencies.                                                                                          |
| `GET /ready`   | Readiness — 200 when all configured dependencies pass their checks, 503 with a stable reason code otherwise. Internal error detail never leaks. |
| `GET /version` | Build info: name, version, commit, Go version, protocol/schema versions.                                                                        |
| `GET /ws`      | WebSocket seam (below).                                                                                                                         |

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
WebSocket → Connection → Message Decoder → Protocol → (future) Game/Lobby Handler
```

v0.1 implements transport honestly:

1. Client must send `hello` first; anything else gets
   `error{code:"malformed_message"}`.
2. Wrong protocol major version → `unsupported_version`.
3. `ping` → `pong{nonce}`.
4. Game actions → structured `not_supported` error carrying `ackSeq`.

It never masquerades as gameplay. The decoder lives in `internal/protocol`
and is fixture-tested against `packages/protocol/fixtures/messages/`.

## Observability

Structured JSON logs on stdout with stable fields: `time`, `level`,
`msg`, `service`, `version`, plus `requestId` on request-scoped lines.

Path for the future: metrics/tracing should hook into the middleware chain
and the engine's event stream; no Prometheus/OpenTelemetry stack is
deployed at this stage by design. CORS is intentionally not opened from the
API server; browser clients will talk to it through the edge/proxy, where
origin policy belongs.

## Authentication boundary

Authentication is **not implemented** and nothing fakes it. Its future home
is a dedicated `internal/auth` package providing connection identity to the
realtime layer (which currently derives no identity — actions receive
`not_supported`). Protocol envelopes already avoid trusting client-supplied
identity, so introducing auth later does not require wire changes.

## Persistence

See migrations in `server/migrations/` and the runner in
`internal/persistence`. Details and developer workflow:
[SETUP.md](../development/SETUP.md).
