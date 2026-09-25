package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/analeis/highjack/server/internal/game"
)

// DecodeGameAction converts a validated wire action payload into the typed
// domain action the engine executes. Unknown types and malformed payloads
// become protocol errors with stable codes; domain rules (turns, phases,
// funds) stay in the engine, which rejects them without mutation.
func DecodeGameAction(payload json.RawMessage) (game.Action, *ProtocolError) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(payload, &generic); err != nil {
		return nil, &ProtocolError{CodeMalformedMessage, "action payload is not a JSON object"}
	}
	typeRaw, ok := generic["type"]
	if !ok {
		return nil, &ProtocolError{CodeMalformedMessage, "action requires \"type\""}
	}
	var typ string
	if err := json.Unmarshal(typeRaw, &typ); err != nil {
		return nil, &ProtocolError{CodeMalformedMessage, "\"type\" must be a string"}
	}

	switch game.ActionType(typ) {
	case game.ActionPlayerJoin:
		var body struct {
			DisplayName string `json:"displayName"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			return nil, &ProtocolError{CodeMalformedMessage, "player_join has an invalid payload"}
		}
		if strings.TrimSpace(body.DisplayName) == "" {
			return nil, &ProtocolError{CodeMalformedMessage, "player_join requires a non-empty \"displayName\""}
		}
		return game.PlayerJoinAction{DisplayName: body.DisplayName}, nil
	case game.ActionPlayerLeave:
		return game.PlayerLeaveAction{}, nil
	case game.ActionPlayerReady:
		var body struct {
			Ready *bool `json:"ready"`
		}
		if err := json.Unmarshal(payload, &body); err != nil || body.Ready == nil {
			return nil, &ProtocolError{CodeMalformedMessage, "player_ready requires a boolean \"ready\""}
		}
		return game.PlayerReadyAction{Ready: *body.Ready}, nil
	case game.ActionGameStart:
		return game.GameStartAction{}, nil
	case game.ActionRollDice:
		return game.RollDiceAction{}, nil
	case game.ActionBuyProperty:
		return game.BuyPropertyAction{}, nil
	case game.ActionDeclineBuy:
		return game.DeclineBuyAction{}, nil
	case game.ActionEndTurn:
		return game.EndTurnAction{}, nil
	default:
		return nil, &ProtocolError{CodeUnknownMessageType,
			fmt.Sprintf("unknown action type %q", typ)}
	}
}
