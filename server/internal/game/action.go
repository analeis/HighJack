package game

import "fmt"

// ActionType names a kind of client-intended state transition.
type ActionType string

const (
	ActionPlayerJoin  ActionType = "player_join"
	ActionPlayerLeave ActionType = "player_leave"
	ActionPlayerReady ActionType = "player_ready"
	ActionGameStart   ActionType = "game_start"
	ActionRollDice    ActionType = "roll_dice"
	ActionBuyProperty ActionType = "buy_property"
	ActionDeclineBuy  ActionType = "decline_buy"
	ActionEndTurn     ActionType = "end_turn"
)

// Action is a request to transition the game state. Actions never mutate
// anything by themselves; the engine validates them against current state
// and performs exactly one atomic transition per accepted action.
//
// Concrete action payloads live in the wire protocol; the engine receives
// already-typed values. Actions do not carry the acting player's id: the
// server derives the actor from the authenticated connection so clients
// cannot spoof identity.
type Action interface {
	Type() ActionType
}

type PlayerJoinAction struct{ DisplayName string }

func (PlayerJoinAction) Type() ActionType { return ActionPlayerJoin }

type PlayerLeaveAction struct{}

func (PlayerLeaveAction) Type() ActionType { return ActionPlayerLeave }

type PlayerReadyAction struct{ Ready bool }

func (PlayerReadyAction) Type() ActionType { return ActionPlayerReady }

type GameStartAction struct{}

func (GameStartAction) Type() ActionType { return ActionGameStart }

// Board-loop actions carry no payload: the authoritative turn state
// already identifies the roller, the space, and the decision. There is no
// property id to forge and no amount to manipulate.
type RollDiceAction struct{}

func (RollDiceAction) Type() ActionType { return ActionRollDice }

type BuyPropertyAction struct{}

func (BuyPropertyAction) Type() ActionType { return ActionBuyProperty }

type DeclineBuyAction struct{}

func (DeclineBuyAction) Type() ActionType { return ActionDeclineBuy }

type EndTurnAction struct{}

func (EndTurnAction) Type() ActionType { return ActionEndTurn }

// Domain errors. The API layer maps these onto protocol error codes;
// they must remain transport-agnostic.

var (
	ErrUnknownAction   = fmt.Errorf("unknown action type")
	ErrOutOfPhase      = fmt.Errorf("action not permitted in the current game phase")
	ErrNotPermitted    = fmt.Errorf("actor is not permitted to perform this action")
	ErrGameFull        = fmt.Errorf("match is full")
	ErrAlreadyStarted  = fmt.Errorf("match has already started")
	ErrNotAllReady     = fmt.Errorf("not all players are ready")
	ErrInvalidName     = fmt.Errorf("display name is invalid")
	ErrPlayerNotInGame = fmt.Errorf("player is not part of this match")
	ErrNoPlayers       = fmt.Errorf("match has no players")
	ErrNotYourTurn     = fmt.Errorf("it is not the actor's turn")
)

// Ruleset is the pluggable behavior layer of the engine: it binds each
// supported action type to its validation + transition logic for a given
// family of game rules. A future board-game ruleset registers additional
// actions without touching the engine core.
type Ruleset interface {
	Name() string
	// Handle applies one validated action transition. Implementations must:
	//   - return Err* domain errors for invalid actions,
	//   - never mutate the input state,
	//   - emit events that correspond exactly to what changed,
	//   - advance the tick by exactly one on success (the engine owns this).
	Handle(state *GameState, cfg *GameConfig, actor PlayerID, action Action, rng Rng) (*GameState, []Event, error)
}
