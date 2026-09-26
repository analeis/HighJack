package game

// LegalActions answers "what could this player do right now?" for one actor.
//
// It exists because the alternative was the client re-deriving the rules. The
// client used to decide affordability, turn ownership and start eligibility with
// its own copies of the engine's predicates, and those copies drifted: the UI
// offered a Start the server would reject, disabled a Decline the server
// accepted, and could wedge a turn with every button dead and no error. Nothing
// in the protocol said what was legal, so there was no single answer to be wrong
// in one place.
//
// The predicates below therefore reuse the same conditions as the handlers, and
// TestLegalActionsMatchApply is the real guarantee: for every state it walks,
// each advertised action must be accepted by Apply, and each action that is not
// advertised must be rejected. A divergence between this file and the ruleset
// fails that test rather than shipping.

import "slices"

// LegalActions returns the action types the engine would accept from actor in
// this state, in a stable order.
//
// The order is fixed rather than map-ordered so the wire representation and the
// client's rendering are deterministic.
func LegalActions(state *GameState, cfg *GameConfig, actor PlayerID) []ActionType {
	var out []ActionType
	add := func(t ActionType) { out = append(out, t) }

	if actor != "" {
		p := state.PlayerByID(actor)
		if p == nil {
			// Not in the match: nothing is permitted, not even leaving.
			return nil
		}
		switch state.Phase {
		case PhaseLobby:
			add(ActionPlayerLeave)
			// Always legal in the lobby, because the toggle always changes
			// something: a player who is ready can un-ready. Omitting it when
			// already ready advertised "nothing to do" while the engine accepted
			// the action, and left a player who inherited the host role with no way
			// to withdraw from a match they no longer wanted to play.
			add(ActionPlayerReady)
			if p.IsHost && canStart(state, cfg) {
				add(ActionGameStart)
			}
		case PhasePlaying:
			add(ActionPlayerLeave)
			// Board actions are the current seat holder's alone.
			if !p.Active() || p.Seat != state.Turn.CurrentSeat || !p.PositionInBounds(len(state.Board.Spaces)) {
				break
			}
			switch state.Turn.Phase {
			case TurnAwaitRoll:
				add(ActionRollDice)
			case TurnAwaitBuyDecision:
				space, ok := currentSpaceOf(state, p)
				if ok && space.Kind == SpaceProperty && !space.Owned {
					// Declining has no precondition beyond being offered the
					// decision, so it stays available even when the price is out of
					// reach. Gating it on affordability made a legal action
					// unreachable and could strand the turn.
					add(ActionDeclineBuy)
					if p.Money >= space.Price {
						add(ActionBuyProperty)
					}
				}
			case TurnOver:
				add(ActionEndTurn)
			}
		default:
			// Ended and Interrupted are terminal: nothing is permitted.
		}
	}
	return out
}

// IsLegal reports whether the engine would accept this action from actor now.
func IsLegal(state *GameState, cfg *GameConfig, actor PlayerID, action Action) bool {
	return slices.Contains(LegalActions(state, cfg, actor), action.Type())
}

// canStart mirrors the start handler's preconditions: enough active players, and
// all of them ready.
func canStart(state *GameState, cfg *GameConfig) bool {
	if state.Phase != PhaseLobby {
		return false
	}
	active := state.ActivePlayers()
	if len(active) < cfg.PlayerCount.Min || len(active) > cfg.PlayerCount.Max {
		return false
	}
	for _, p := range active {
		if !p.Ready {
			return false
		}
	}
	return true
}

// currentSpaceOf is the space a player stands on, without erroring.
func currentSpaceOf(state *GameState, p *Player) (RuntimeSpace, bool) {
	if p == nil || !p.PositionInBounds(len(state.Board.Spaces)) {
		return RuntimeSpace{}, false
	}
	return state.Board.Spaces[p.Position], true
}
