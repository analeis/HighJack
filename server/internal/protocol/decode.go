package protocol

import (
	"encoding/json"
	"fmt"

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
		// Refused at the transport boundary, and deliberately not merely
		// unsupported. The engine's join handler derives the new player's identity
		// from the state, ignoring the actor, so a player_join arriving on an
		// already-bound socket would mint a player that has no reconnect token and
		// can never be bound or removed — while consuming one of the match's
		// seats. Because it is unauthenticated and cannot be undone, repeating it
		// would fill the match permanently.
		//
		// Seats are minted by the lobby API (POST /matches/{id}/players), which is
		// the only path that also issues the token. ActionPlayerJoin stays in the
		// action union because it is part of the engine's internal contract, but it
		// is not a wire action a client may send.
		return nil, &ProtocolError{
			Code: CodeNotPermitted,
			Msg:  "player_join is not accepted on the socket; claim a seat through POST /matches/{matchId}/players",
		}
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
