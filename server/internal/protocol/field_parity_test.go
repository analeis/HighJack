package protocol_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/protocol"
)

// Field-level parity between the TypeScript contract and the Go wire.
//
// The vocabulary parity tests in parity_test.go compare the *names* the two
// sides use: event types, error codes, phases, action types. They say nothing
// about the *shape* of an event, and that gap let the two disagree in two
// ways nothing could see.
//
// The TypeScript package declared `GameStartedEvent.seed` as a required field.
// The Go server deliberately removes the seed from the wire — it derives every
// future roll from it, so publishing it would let any participant predict the
// rest of the match — and `isGameEvent` therefore rejected every
// `game_started` event the server actually sent.
//
// In the other direction, the Go server sends `releasedSpaces` on a bankruptcy
// and the TypeScript contract did not describe it at all, so a client reading
// the contract could not know the field existed.
//
// This file compares the fields of each event as it appears *on the wire*
// against the fields the shared TypeScript contract declares. It encodes a
// fully populated sample of every event with the production encoder, so
// anything the encoder strips, and anything `omitempty` can drop, is accounted
// for by construction rather than by a special case here.

const eventsTS = "events.ts"

// eventSamples maps each event type to a zero-valued instance of the Go type
// that produces it. The sample is populated by reflection before it is
// encoded, so every field — including the `omitempty` ones — is present.
var eventSamples = map[string]game.Event{
	"player_joined":        &game.PlayerJoinedEvent{},
	"player_left":          &game.PlayerLeftEvent{},
	"player_ready_changed": &game.PlayerReadyChangedEvent{},
	"game_started":         &game.GameStartedEvent{},
	"player_eliminated":    &game.PlayerEliminatedEvent{},
	"game_ended":           &game.GameEndedEvent{},
	"dice_rolled":          &game.DiceRolledEvent{},
	"property_bought":      &game.PropertyBoughtEvent{},
	"buy_declined":         &game.BuyDeclinedEvent{},
	"rent_paid":            &game.RentPaidEvent{},
	"bank_transfer":        &game.BankTransferEvent{},
	"player_bankrupt":      &game.PlayerBankruptEvent{},
	"turn_advanced":        &game.TurnAdvancedEvent{},
}

// tsEventFields extracts, for each event interface in events.ts, the event type
// it discriminates on, the fields it requires, and the fields it marks
// optional.
//
// The source is read rather than a build artifact, for the same reason the
// vocabulary parity tests read it: it needs no toolchain beyond Go, and it
// fails on the exact commit that introduced the drift.
func tsEventFields(t *testing.T) map[string]tsEvent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(tsRoot, eventsTS))
	if err != nil {
		t.Fatalf("read %s: %v", eventsTS, err)
	}
	src := string(raw)

	ifaceRe := regexp.MustCompile(`export interface (\w+) extends EventBase \{([^}]*)\}`)
	fieldRe := regexp.MustCompile(`readonly (\w+)(\??):`)

	out := map[string]tsEvent{}
	for _, m := range ifaceRe.FindAllStringSubmatch(src, -1) {
		body := m[2]
		typeRe := regexp.MustCompile(`readonly type:\s*'(\w+)'`)
		tm := typeRe.FindStringSubmatch(body)
		if tm == nil {
			t.Fatalf("event interface %s does not discriminate on a type literal", m[1])
		}
		eventType := tm[1]
		ev := tsEvent{required: map[string]bool{}, optional: map[string]bool{}}
		for _, fm := range fieldRe.FindAllStringSubmatch(body, -1) {
			name, optional := fm[1], fm[2] == "?"
			if name == "type" {
				continue // the discriminator itself
			}
			if optional {
				ev.optional[name] = true
			} else {
				ev.required[name] = true
			}
		}
		if _, dup := out[eventType]; dup {
			t.Fatalf("events.ts declares the %q event twice", eventType)
		}
		out[eventType] = ev
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no event interfaces", eventsTS)
	}
	return out
}

// tsEvent is the shape the TypeScript contract declares for one event.
type tsEvent struct {
	required map[string]bool
	optional map[string]bool
}

// populate sets every exported field of an event to a non-zero value, so that
// a field tagged `omitempty` is actually present on the wire.
//
// A zero-valued sample cannot see an `omitempty` field at all, which is how
// this test first reported a divergence that was not one.
func populate(ev game.Event) {
	v := reflect.ValueOf(ev).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported, including the embedded eventBase
		}
		setNonZero(v.Field(i))
	}
}

// setNonZero assigns a non-zero value of the field's own type.
func setNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		setNonZero(v.Index(0))
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		setNonZero(v.Elem())
	}
}

// wireFields returns the field names an event actually has on the wire.
func wireFields(t *testing.T, ev game.Event) []string {
	t.Helper()
	payload, err := protocol.EncodeEvent(ev)
	if err != nil {
		t.Fatalf("encode %s: %v", ev.Type(), err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode %s payload %q: %v", ev.Type(), payload, err)
	}
	var names []string
	for name := range fields {
		if name == "type" {
			continue // added by the encoder
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestEventFieldsMatchTypeScript compares each event's wire shape with the
// shared TypeScript contract.
//
// Two rules, and they are the two ways the two sides actually diverged:
//
//   - Every field the server puts on the wire must be described by the
//     contract. A client that reads the contract must be able to read every
//     field it receives.
//   - Every field the contract *requires* must be one the server sends. A
//     contract that demands a field the server removes rejects every event of
//     that type, which is what the seed did.
func TestEventFieldsMatchTypeScript(t *testing.T) {
	ts := tsEventFields(t)

	for eventType, ev := range ts {
		sample, ok := eventSamples[eventType]
		if !ok {
			t.Fatalf("events.ts declares the %q event, which the Go side does not produce", eventType)
		}
		populate(sample)
		wire := map[string]bool{}
		for _, name := range wireFields(t, sample) {
			wire[name] = true
		}

		// The server sends a field the contract does not describe.
		for name := range wire {
			if !ev.required[name] && !ev.optional[name] {
				t.Fatalf("the %q event carries the field %q on the wire, but the shared "+
					"contract does not describe it", eventType, name)
			}
		}

		// The contract requires a field the server does not send.
		for name := range ev.required {
			if !wire[name] {
				t.Fatalf("the shared contract requires %q on the %q event, but the server "+
					"never sends it", name, eventType)
			}
		}
	}

	// The other direction: an event the server produces that the contract does
	// not describe at all.
	for eventType := range eventSamples {
		if _, ok := ts[eventType]; !ok {
			t.Fatalf("the Go side produces a %q event that events.ts does not describe", eventType)
		}
	}
}

// TestEveryEventTypeIsCovered guards the two directions above against the
// possibility that both sides simply agree on a smaller set.
func TestEveryEventTypeIsCovered(t *testing.T) {
	ts := tsEventFields(t)
	if len(ts) != len(eventSamples) {
		t.Fatalf("the parity test covers %d event types but the samples hold %d",
			len(ts), len(eventSamples))
	}
	for _, want := range []string{
		"player_joined", "player_left", "player_ready_changed", "game_started",
		"player_eliminated", "game_ended", "dice_rolled", "property_bought",
		"buy_declined", "rent_paid", "bank_transfer", "player_bankrupt",
		"turn_advanced",
	} {
		if _, ok := ts[want]; !ok {
			t.Fatalf("events.ts does not describe the %q event", want)
		}
	}
}

// TestGameStartedCarriesNoSeed is the specific regression, kept as its own
// test so that a failure names the security property rather than a field set.
//
// The root seed determines every future roll. A client that receives it can
// predict the rest of the match, which is why the encoder removes it. That
// removal has to survive: the field lives on the durable event, where it
// belongs, and not on the wire.
func TestGameStartedCarriesNoSeed(t *testing.T) {
	payload, err := protocol.EncodeEvent(&game.GameStartedEvent{ConfigHash: "abc", Seed: "deadbeef"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := fields["seed"]; present {
		t.Fatalf("the wire carries the match seed: %s", payload)
	}
	if _, present := fields["configHash"]; !present {
		t.Fatalf("the wire lost the config hash: %s", payload)
	}
}

// TestTypeScriptDoesNotRequireTheSeed is the other half of the same
// regression: the shared contract must not *require* a field the server
// removes, or every consumer that validates events rejects them.
func TestTypeScriptDoesNotRequireTheSeed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(tsRoot, eventsTS))
	if err != nil {
		t.Fatalf("read %s: %v", eventsTS, err)
	}
	src := string(raw)
	start := strings.Index(src, "export interface GameStartedEvent")
	if start < 0 {
		t.Fatal("events.ts does not declare GameStartedEvent")
	}
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatal("GameStartedEvent is not terminated")
	}
	body := src[start : start+end]
	if strings.Contains(body, "readonly seed") {
		t.Fatalf("the shared contract still requires a seed field: %s", body)
	}
}
