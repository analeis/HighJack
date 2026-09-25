package game

import (
	"fmt"
	"strings"
)

// Engine executes a ruleset against game state deterministically.
//
//	Apply(state, actor, action) → (newState, events, error)
//
// The engine owns nothing but logic: no sockets, no storage, no clocks.
// A failed Apply guarantees zero mutation — invalid actions cannot change
// state. A successful Apply produces exactly one new state whose tick is
// the previous tick plus one, plus the events that describe the change.
type Engine struct {
	config *GameConfig
	rules  Ruleset
	root   Seed
}

// NewEngine builds an engine for the given configuration and root seed.
// The config must be valid; an invalid config is a programmer/server error,
// not something the engine silently repairs.
func NewEngine(cfg *GameConfig, root Seed, rules Ruleset) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("engine requires a config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("engine config rejected: %w", err)
	}
	if rules == nil {
		return nil, fmt.Errorf("engine requires a ruleset")
	}
	return &Engine{config: cfg, rules: rules, root: root}, nil
}

func (e *Engine) Config() *GameConfig { return e.config }

func (e *Engine) RootSeed() Seed { return e.root }

// NewState returns the pristine pre-match state for a match id.
func (e *Engine) NewState(id GameID) *GameState {
	return &GameState{
		ID:          id,
		Phase:       PhaseLobby,
		RootSeedHex: e.root.Hex(),
		Players:     []Player{},
	}
}

// Apply validates and applies one action atomically.
func (e *Engine) Apply(state *GameState, actor PlayerID, action Action) (*GameState, []Event, error) {
	if state == nil {
		return nil, nil, fmt.Errorf("state must not be nil")
	}
	if action == nil {
		return nil, nil, fmt.Errorf("action must not be nil")
	}
	if !state.Phase.Valid() {
		return nil, nil, fmt.Errorf("corrupt state: unknown phase %q", state.Phase)
	}

	next, events, err := e.rules.Handle(state, e.config, actor, action, NewSeededRng(Split(e.root, fmt.Sprintf("tick:%d", state.Tick))))
	if err != nil {
		return nil, nil, err
	}

	next.Tick = state.Tick + 1
	for i := range events {
		events[i] = stamp(events[i], next.Tick)
	}
	return next, events, nil
}

func stamp(ev Event, tick uint64) Event {
	switch e := ev.(type) {
	case *PlayerJoinedEvent:
		e.tick = tick
		return e
	case *PlayerLeftEvent:
		e.tick = tick
		return e
	case *PlayerReadyChangedEvent:
		e.tick = tick
		return e
	case *GameStartedEvent:
		e.tick = tick
		return e
	case *PlayerEliminatedEvent:
		e.tick = tick
		return e
	case *GameEndedEvent:
		e.tick = tick
		return e
	case *DiceRolledEvent:
		e.tick = tick
		return e
	case *PropertyBoughtEvent:
		e.tick = tick
		return e
	case *BuyDeclinedEvent:
		e.tick = tick
		return e
	case *RentPaidEvent:
		e.tick = tick
		return e
	case *BankTransferEvent:
		e.tick = tick
		return e
	case *PlayerBankruptEvent:
		e.tick = tick
		return e
	case *TurnAdvancedEvent:
		e.tick = tick
		return e
	default:
		panic(fmt.Sprintf("game: unstampable event type %T", ev))
	}
}

// DerivePlayerID deterministically mints the player id for a given match
// seed and seat so that replays reproduce identical ids.
func DeriveMatchPlayerID(matchSeed Seed, seat Seat) PlayerID {
	child := Split(matchSeed, fmt.Sprintf("player:%d", seat))
	hexed := child.Hex()
	return PlayerID("pl_" + strings.ToUpper(hexed[:20]))
}
