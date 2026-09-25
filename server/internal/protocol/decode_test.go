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
		{"actions/player_join.json", game.ActionPlayerJoin},
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

func TestDecodeGameActionPayloads(t *testing.T) {
	join, perr := DecodeGameAction(json.RawMessage(`{"type":"player_join","displayName":"Ace"}`))
	if perr != nil {
		t.Fatalf("join rejected: %+v", perr)
	}
	if got := join.(game.PlayerJoinAction).DisplayName; got != "Ace" {
		t.Fatalf("displayName = %q", got)
	}

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
		{"join without name", `{"type":"player_join"}`, CodeMalformedMessage},
		{"join blank name", `{"type":"player_join","displayName":"  "}`, CodeMalformedMessage},
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
