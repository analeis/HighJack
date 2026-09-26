package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
)

// fixturesDir resolves the shared wire fixtures owned by packages/protocol.
const fixturesDir = "../../../packages/protocol/fixtures/"

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

func TestDecodeGameActionFromSharedFixtures(t *testing.T) {
	cases := []struct {
		fixture string
		want    game.ActionType
	}{
		// player_join is deliberately absent: it is an engine action type but not a
		// wire action. Seats are minted by the lobby API, which is also the only
		// path that issues a reconnect token, so the socket refuses it. See
		// TestPlayerJoinIsRefusedOnTheWire.
		{"actions/player_leave.json", game.ActionPlayerLeave},
		{"actions/player_ready.json", game.ActionPlayerReady},
		{"actions/game_start.json", game.ActionGameStart},
		{"actions/roll_dice.json", game.ActionRollDice},
		{"actions/buy_property.json", game.ActionBuyProperty},
		{"actions/decline_buy.json", game.ActionDeclineBuy},
		{"actions/end_turn.json", game.ActionEndTurn},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			action, perr := DecodeGameAction(readFixture(t, tc.fixture))
			if perr != nil {
				t.Fatalf("fixture rejected: %+v", perr)
			}
			if action.Type() != tc.want {
				t.Fatalf("type = %q, want %q", action.Type(), tc.want)
			}
		})
	}
}

// A player_join action frame must be refused, whatever its payload. The engine's
// join handler derives the new player's identity from state and ignores the
// actor, so accepting one on an already-bound socket would mint a player with no
// reconnect token that can never be bound or removed, while consuming a seat. It
// is unauthenticated and irreversible, so repeating it fills the match for good.
func TestPlayerJoinIsRefusedOnTheWire(t *testing.T) {
	for _, raw := range []string{
		`{"type":"player_join","displayName":"Ace"}`,
		`{"type":"player_join"}`,
		`{"type":"player_join","displayName":"  "}`,
		`{"type":"player_join","displayName":"Ace","extra":1}`,
	} {
		action, perr := DecodeGameAction(json.RawMessage(raw))
		if perr == nil {
			t.Fatalf("%s was accepted as %#v; a socket must never mint a player", raw, action)
		}
		if perr.Code != CodeNotPermitted {
			t.Fatalf("%s: code = %s, want not_permitted", raw, perr.Code)
		}
	}
	// The shared fixture for the historical shape is refused too, so the wire
	// contract cannot quietly drift back to accepting it.
	action, perr := DecodeGameAction(readFixture(t, "actions/player_join.json"))
	if perr == nil {
		t.Fatalf("the player_join fixture was accepted as %#v", action)
	}
}

func TestDecodeGameActionPayloads(t *testing.T) {
	ready, perr := DecodeGameAction(json.RawMessage(`{"type":"player_ready","ready":true}`))
	if perr != nil {
		t.Fatalf("ready rejected: %+v", perr)
	}
	if !ready.(game.PlayerReadyAction).Ready {
		t.Fatal("ready must decode true")
	}
}

func TestDecodeGameActionRejectsMalformedAndUnknown(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ErrorCode
	}{
		{"not json", `{`, CodeMalformedMessage},
		{"array", `[1]`, CodeMalformedMessage},
		{"missing type", `{"displayName":"Ace"}`, CodeMalformedMessage},
		{"type not string", `{"type":5}`, CodeMalformedMessage},
		// A player_join is refused as not-permitted regardless of payload, so the
		// payload is never inspected; see TestPlayerJoinIsRefusedOnTheWire.
		{"join", `{"type":"player_join"}`, CodeNotPermitted},
		{"join blank name", `{"type":"player_join","displayName":"  "}`, CodeNotPermitted},
		{"ready without flag", `{"type":"player_ready"}`, CodeMalformedMessage},
		{"unknown type", `{"type":"teleport"}`, CodeUnknownMessageType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, perr := DecodeGameAction(json.RawMessage(tc.raw))
			if perr == nil {
				t.Fatalf("expected rejection, got %#v", action)
			}
			if perr.Code != tc.want {
				t.Fatalf("code = %s, want %s (%s)", perr.Code, tc.want, perr.Msg)
			}
		})
	}
}

func TestEventFixturesEncodeIdentically(t *testing.T) {
	// Every new event type must marshal to the exact shared fixture bytes
	// (key order included after a canonical re-encode), pinning the Go and
	// TypeScript wire shapes.
	cases := []struct {
		fixture string
		event   game.Event
	}{
		{"events/dice_rolled.json", &game.DiceRolledEvent{
			PlayerID: "pl_78FB853327D328A5343B", Die1: 3, Die2: 4,
			FromSpace: "go", ToSpace: "n2", PassedGo: false,
		}},
		{"events/property_bought.json", &game.PropertyBoughtEvent{
			PlayerID: "pl_78FB853327D328A5343B", SpaceID: "a1", Price: 100,
		}},
		{"events/buy_declined.json", &game.BuyDeclinedEvent{
			PlayerID: "pl_78FB853327D328A5343B", SpaceID: "a1",
		}},
		{"events/rent_paid.json", &game.RentPaidEvent{
			FromPlayerID: "pl_78FB853327D328A5343B", ToPlayerID: "pl_9C04D2A1B6E5F7081923",
			SpaceID: "a1", Amount: 10,
		}},
		{"events/bank_transfer.json", &game.BankTransferEvent{
			PlayerID: "pl_78FB853327D328A5343B", Amount: 200,
			Direction: "to_player", Reason: "pass_go",
		}},
		{"events/player_bankrupt.json", &game.PlayerBankruptEvent{
			PlayerID: "pl_78FB853327D328A5343B", Cause: "bankruptcy_rent",
			CreditorID: "pl_9C04D2A1B6E5F7081923", AmountOwed: 40,
		}},
		{"events/turn_advanced.json", &game.TurnAdvancedEvent{Seat: 1, Round: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got, err := EncodeEvent(tc.event)
			if err != nil {
				t.Fatal(err)
			}
			var gotMap, wantMap any
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(readFixture(t, tc.fixture), &wantMap); err != nil {
				t.Fatal(err)
			}
			gotNorm, _ := json.Marshal(gotMap)
			wantNorm, _ := json.Marshal(wantMap)
			if string(gotNorm) != string(wantNorm) {
				t.Fatalf("event shape diverged from fixture:\n go:  %s\nwant: %s", gotNorm, wantNorm)
			}
		})
	}
}
