package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/analeis/highjack/server/internal/game"
)

// EncodeEvent serializes a domain event into the wire shape shared with
// @highjack/protocol: the event's fields plus the `type` discriminator the
// discriminated-union contract requires.
//
// The discriminator lives here, in the transport boundary, not on the
// engine's Event interface: the engine stays transport-agnostic and does
// not need to know it has a wire representation. Encoding goes through a
// map, so keys are emitted in sorted order (JSON key order is not
// significant; shared fixtures are compared after canonicalization).
func EncodeEvent(ev game.Event) ([]byte, error) {
	if ev == nil {
		return nil, fmt.Errorf("protocol: cannot encode nil event")
	}
	fields, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("encode event %s: %w", ev.Type(), err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(fields, &out); err != nil {
		return nil, fmt.Errorf("re-decode event %s: %w", ev.Type(), err)
	}
	discriminator, err := json.Marshal(ev.Type())
	if err != nil {
		return nil, err
	}
	out["type"] = discriminator
	// The match seed is durable replay/debug metadata, never client-visible state:
	// NewGameSnapshot deliberately omits it, and the durable event payload is
	// where it belongs. Broadcasting it leaked a value the codebase classifies as
	// server-secret to every participant, including eliminated ones, and recorded
	// that disclosure in the wire contract itself.
	if _, isGameStarted := ev.(*game.GameStartedEvent); isGameStarted {
		delete(out, "seed")
	}
	return json.Marshal(out)
}
