package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
)

// The match seed is durable replay metadata, never client-visible state.
// NewGameSnapshot deliberately omits it; the wire encoder must too, or the value
// is handed to every participant — including eliminated ones — and the
// contract itself records the disclosure.
func TestEncodeEventStripsTheServerOnlyMatchSeed(t *testing.T) {
	payload, err := EncodeEvent(&game.GameStartedEvent{ConfigHash: "h", Seed: "deadbeef"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["seed"]; present {
		t.Fatalf("the match seed must not be client-visible, got %v", got)
	}
	if got["type"] != string(game.EventGameStarted) {
		t.Fatalf("type = %v, want game_started", got["type"])
	}
	if got["configHash"] != "h" {
		t.Fatalf("configHash = %v, want h", got["configHash"])
	}

	// The durable payload keeps it: it is what a replay verifier would need. This
	// is the whole point of the split.
	raw, err := game.MarshalEvent(&game.GameStartedEvent{ConfigHash: "h", Seed: "deadbeef"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "deadbeef") {
		t.Fatal("the durable event payload must retain the seed")
	}
}
