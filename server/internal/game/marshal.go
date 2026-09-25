package game

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalGameState serializes authoritative state for durable snapshots.
// GameState is already a plain serializable value (no transport, clock, or
// engine internals), so persistence needs no custom mapping.
func MarshalGameState(state *GameState) ([]byte, error) {
	if state == nil {
		return nil, fmt.Errorf("game: cannot marshal nil state")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}
	return data, nil
}

// UnmarshalGameState restores state from a durable snapshot. The resulting
// state is revalidated on the way in: corrupt snapshots are rejected rather
// than silently resumed.
func UnmarshalGameState(data []byte) (*GameState, error) {
	var state GameState
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	if !state.Phase.Valid() {
		return nil, fmt.Errorf("unmarshal state: unknown phase %q", state.Phase)
	}
	if !state.Turn.Phase.Valid() {
		return nil, fmt.Errorf("unmarshal state: unknown turn phase %q", state.Turn.Phase)
	}
	return &state, nil
}

// MarshalEvent serializes an event's fields. The type discriminator is not
// included: callers that persist events record ev.Type() alongside the
// payload (the transport package adds "type" for the wire shape).
func MarshalEvent(ev Event) ([]byte, error) {
	if ev == nil {
		return nil, fmt.Errorf("game: cannot marshal nil event")
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return data, nil
}
