package game

// MatchRuleset is the complete v0.2 match behavior: the v0.1 lobby
// lifecycle plus the board loop. It routes each action to the owning
// ruleset by type so neither ruleset needs to know about the other, and
// the engine core stays untouched.
type MatchRuleset struct {
	Lifecycle LifecycleRuleset
	Board     BoardRuleset
}

func (MatchRuleset) Name() string { return "match/v1" }

func (r MatchRuleset) Handle(state *GameState, cfg *GameConfig, actor PlayerID, action Action, rng Rng) (*GameState, []Event, error) {
	switch action.(type) {
	case PlayerJoinAction, PlayerLeaveAction, PlayerReadyAction, GameStartAction:
		return r.Lifecycle.Handle(state, cfg, actor, action, rng)
	case RollDiceAction, BuyPropertyAction, DeclineBuyAction, EndTurnAction:
		return r.Board.Handle(state, cfg, actor, action, rng)
	default:
		return nil, nil, ErrUnknownAction
	}
}
