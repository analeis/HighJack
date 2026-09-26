# Protocol Overview

> Status: v0.1.0 Foundation. The contract below is implemented, fixture-
> tested on both sides, and stable for future transport work.

## Where it lives

| Side                                       | Location                                                   |
| ------------------------------------------ | ---------------------------------------------------------- |
| TypeScript (canonical docs + client types) | `packages/protocol`                                        |
| Go (server mirror)                         | `server/internal/protocol` + engine event/action JSON tags |
| Shared fixtures (compatibility tests)      | `packages/protocol/fixtures/`                              |

Both implementations decode the same fixtures; the canonical-JSON config
hash must be byte-identical (tested with `fixtures/config/canonical_hash_case.json`).

## Versioning

- `PROTOCOL_VERSION = "1.1.0"` — semver. **Major = wire compatibility.**
  Clients and servers with the same major interoperate. v0.2 added the
  board-loop vocabulary, `hello` match binding, `ack`, and the
  snapshot/catchup envelopes: additive, so only the minor moved.
- Every envelope carries `v: 1` (major as integer) so a mismatch is
  rejected before payload parsing.
- `SCHEMA_VERSION = 1` — monotonic revision of payload schemas, embedded in
  fixtures and asserted equal across TS/Go.

Rules: never rename or repurpose an existing field or error code. Additive
changes bump minor; breaking changes bump major and require a migration
note here.

## Envelopes

One JSON object per WebSocket text frame.

```jsonc
// client → server
{ "v": 1, "type": "hello",  "client": "web/0.1.0" }
{ "v": 1, "type": "ping",   "nonce": 42 }
{ "v": 1, "type": "action", "seq": 7, "action": { … } }

// server → client
{ "v": 1, "type": "welcome", "session": "…", "protocolVersion": "1.0.0", "heartbeatMs": 30000 }
{ "v": 1, "type": "pong",    "nonce": 42 }
{ "v": 1, "type": "event",   "tick": 12, "event": { … } }
{ "v": 1, "type": "error",   "error": { "code": "…", "message": "…" }, "ackSeq": 7 }
```

Design notes:

- Actions carry no player identity — the server derives the actor from the
  connection. Clients cannot spoof each other.
- `seq` / `ackSeq` correlate errors to the offending action per connection.
- Events carry the engine tick, which defines total causal order. No
  wall-clock timestamps inside simulation output.

## Action & event vocabulary (v0.1)

Actions: `player_join`, `player_leave`, `player_ready`, `game_start`,
`roll_dice`, `buy_property`, `decline_buy`, `end_turn`.

Events: `player_joined`, `player_left`, `player_ready_changed`,
`game_started`, `player_eliminated`, `game_ended`, `dice_rolled`,
`property_bought`, `buy_declined`, `rent_paid`, `bank_transfer`,
`player_bankrupt`, `turn_advanced`.

This is the real vocabulary implemented by the engine. Future systems
extend both unions additively.

Board actions carry **no payload**: the authoritative turn state already
identifies the roller, the space, and the decision, so a client cannot
forge a property id or an amount.

## Server → client envelopes (v0.2)

| Type       | Meaning                                                                |
| ---------- | ---------------------------------------------------------------------- |
| `welcome`  | handshake accepted; carries the session and, when bound, the match id  |
| `pong`     | ping answered with the same nonce                                      |
| `event`    | one authoritative event with the engine tick that produced it          |
| `ack`      | action accepted; carries the **post-action authoritative snapshot**    |
| `error`    | structured rejection with a stable code (and `ackSeq` when applicable) |
| `snapshot` | full authoritative state; sent on bind, reconnect, and on request      |
| `catchup`  | retained events after a client cursor, when that cursor is still valid |

`ack` carrying a snapshot is what makes "an ack is proof of acceptance"
true for the client: the UI renders server state, never its own guess.
Delivery of `event` is at-least-once; the engine tick is the dedup key.

## Error codes

`malformed_message`, `unsupported_version`, `unknown_message_type`,
`invalid_action`, `not_permitted`, `out_of_phase`, `game_full`,
`already_started`, `rate_limited`, `not_supported`, `internal_error`.

## Recovery and identity (v0.2)

- `hello` may carry `matchId`, `token`, and `resumeFromTick`. The token is
  the player's reconnect credential; the server stores only its sha256.
- The actor for every action is the session binding. A payload can never
  name a different player.
- After a bind the server **always** sends a full snapshot. Missed events
  follow only when the supplied cursor is inside the retained window
  (1024 events per match); otherwise `resync: true` tells the client to
  trust the snapshot rather than assume a complete history.
- Action `seq` is per-connection and monotonic. A repeated last `seq`
  replays the cached ack; an older `seq` is rejected as stale.

## GameConfig on the wire

The full schema lives in `packages/protocol/src/config.ts` and is mirrored
by `GameConfig` in Go. Validation tiers (syntactic / structural /
semantic), dotted issue paths, limits, and the default document are shared
via fixtures so clients can pre-validate exactly what the server will
accept.

## Serialization policy: why JSON, and what would change our mind

JSON today because it is debuggable, diffable, and needs no codegen.
Revisit only when measurements demand it:

| Trigger                                                             | Candidate                                                  |
| ------------------------------------------------------------------- | ---------------------------------------------------------- |
| Bandwidth cost of verbose boards/events becomes measurable at scale | MessagePack or CBOR (drop-in value encoding, same schemas) |
| Need for cross-language schema enforcement beyond fixtures          | Protobuf or a JSON-Schema-driven generator                 |
| High-frequency minigame channels (>30 msgs/s/client sustained)      | Binary framing + batching                                  |

Any replacement must preserve: envelope version stamping, actor identity
never coming from the client, and tick-ordered events.
