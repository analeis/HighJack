# Protocol Overview

> Status: v0.2.10. The contract below is implemented, fixture- and
> field-parity-tested on both sides, and stable for future transport work.

## Where it lives

| Side                                       | Location                                                   |
| ------------------------------------------ | ---------------------------------------------------------- |
| TypeScript (canonical docs + client types) | `packages/protocol`                                        |
| Go (server mirror)                         | `server/internal/protocol` + engine event/action JSON tags |
| Shared fixtures (compatibility tests)      | `packages/protocol/fixtures/`                              |

Both implementations decode the same fixtures; the canonical-JSON config
hash must be byte-identical (tested with `fixtures/config/canonical_hash_case.json`).

## Versioning

- `PROTOCOL_VERSION = "1.2.0"` — semver. **Major = wire compatibility.**
  Clients and servers with the same major interoperate. 1.1.0 added the
  board-loop vocabulary, `hello` match binding, `ack`, and the
  snapshot/catchup envelopes. 1.2.0 added the per-recipient `you` block on a
  snapshot and the batched `transition` frame. Both are additive, so only the
  minor moved.
- Every envelope carries `v: 1` (major as integer) so a mismatch is
  rejected before payload parsing.
- `SCHEMA_VERSION = 1` — monotonic revision of payload schemas, embedded in
  fixtures and asserted equal across TS/Go.

Rules: never rename or repurpose an existing field or error code. Additive
changes bump minor; breaking changes bump major and require a migration
note here.

### Parity is checked at the field level, not only the name level

The TypeScript package in `packages/protocol` and the Go mirror in
`server/internal/protocol` are two hand-maintained copies of one contract. Two
suites keep them honest, and they check different things:

- **Vocabulary parity** (`server/internal/protocol/parity_test.go`) compares the
  _names_ the two sides use: event types, error codes, match phases, turn
  phases, action types, space kinds, and the protocol version.
- **Field parity** (`server/internal/protocol/field_parity_test.go`) compares
  the _shape_ of every event as it appears on the wire against the fields the
  TypeScript contract declares. It encodes a fully populated sample of every
  event with the production encoder, so anything the encoder strips, and
  anything `omitempty` can drop, is accounted for by construction.

The second suite exists because the first could not see the only two ways the
two sides actually diverged. The TypeScript contract required a `seed` field on
`game_started` that the server deliberately removes — the seed determines every
future roll, so publishing it would let any participant predict the rest of the
match — which meant `isGameEvent` rejected every `game_started` event the
server actually sent. In the other direction, the server sends `releasedSpaces`
on a bankruptcy and the contract did not describe it, so a client reading the
contract could not know the field existed.

## Envelopes

One JSON object per WebSocket text frame.

```jsonc
// client → server
{ "v": 1, "type": "hello",  "client": "web/0.2.10", "matchId": "m_…", "token": "…", "resumeFromTick": 41 }
{ "v": 1, "type": "ping",   "nonce": 42 }
{ "v": 1, "type": "action", "seq": 7, "action": { … } }

// server → client
{ "v": 1, "type": "welcome",  "session": "…", "protocolVersion": "1.2.0", "heartbeatMs": 30000 }
{ "v": 1, "type": "pong",     "nonce": 42 }
{ "v": 1, "type": "error",    "error": { "code": "…", "message": "…" }, "ackSeq": 7 }
{ "v": 1, "type": "ack",      "ackSeq": 7, "nextSeq": 8, "tick": 42, "snapshot": { … } }
{ "v": 1, "type": "snapshot", "snapshot": { … }, "config": { … }, "nextSeq": 8, "resync": false }
{ "v": 1, "type": "catchup",  "events": [ { "tick": 41, "event": { … } } ], "cursor": 42 }
{ "v": 1, "type": "transition", "tick": 42, "events": [ { "tick": 42, "event": { … } } ],
                           "you": { "playerId": "pl_…", "legalActions": [ "roll_dice" ] } }
```

**The legacy `event` frame.** Before v0.2.2 the server sent one `event` per
frame. It now sends a single batched `transition` frame, and the client still
_tolerates_ a bare `event` — the protocol's major version is still 1, so a
client and a server from different v0.2.x releases interoperate. The server does
not emit `event`, and the shared contract keeps it only for that tolerance.

**The `you` block.** `transition` and `ack` carry a per-recipient `you` block
with the legal actions _for that player_. It is computed by the engine at the
same instant as the frame, so a client never has to re-derive the rules. A
transition therefore differs per recipient, which is why it cannot be a single
shared frame.

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

A transition is delivered as **one batched frame per recipient**, not one frame
per event, so a transition's events cannot interleave with another transition's on
a shared connection. Every event in a transition shares one tick, which makes the
tick the transition's identity on the wire and therefore the client's dedup key;
delivery is at-least-once.

### Legality is server-owned

`snapshot.you` and `transition.you` carry the action types the engine would
accept from _that_ recipient, right now. They are per-recipient because legality
is relative to an actor, and a shared frame cannot serve two players. They ride
on the transition rather than only the snapshot because legality changes when
_anyone_ acts: a second player readying is what unblocks the host's Start, and no
snapshot is sent for another player's action.

A client must not re-derive this. Doing so is what produced a Start the server
refused, a Decline the server accepted, and a turn with every control disabled
and no error. A client that receives no `you` block — a 1.1 server — may derive a
fallback, and that fallback is a compatibility path, not a second source of truth.

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
