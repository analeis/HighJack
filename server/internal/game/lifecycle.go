package game

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// LifecycleRuleset implements the real v0.1 match lifecycle: joining,
// leaving, readying up, and starting. It contains no board gameplay —
// those systems will register their own actions through additional
// rulesets layered on top of the same engine core.
type LifecycleRuleset struct{}

func (LifecycleRuleset) Name() string { return "lifecycle/v1" }

func (r LifecycleRuleset) Handle(state *GameState, cfg *GameConfig, actor PlayerID, action Action, rng Rng) (*GameState, []Event, error) {
	switch a := action.(type) {
	case PlayerJoinAction:
		return r.join(state, cfg, a)
	case PlayerLeaveAction:
		return r.leave(state, actor)
	case PlayerReadyAction:
		return r.ready(state, actor, a)
	case GameStartAction:
		return r.start(state, cfg, actor, rng)
	default:
		return nil, nil, ErrUnknownAction
	}
}

func (r LifecycleRuleset) join(state *GameState, cfg *GameConfig, a PlayerJoinAction) (*GameState, []Event, error) {
	switch state.Phase {
	case PhaseLobby:
		// ok
	case PhasePlaying:
		return nil, nil, ErrAlreadyStarted
	default:
		return nil, nil, ErrOutOfPhase
	}
	if err := validateDisplayName(a.DisplayName); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrInvalidName, err)
	}
	if len(state.Players) >= cfg.PlayerCount.Max {
		return nil, nil, ErrGameFull
	}

	matchSeed := Split(mustParseSeed(state.RootSeedHex), "match")
	seat := state.NextSeat()

	next := state.Clone()
	player := Player{
		ID:     DeriveMatchPlayerID(matchSeed, seat),
		Name:   strings.Clone(a.DisplayName),
		Seat:   seat,
		Money:  cfg.StartingMoney,
		Status: PlayerActive,
		IsHost: len(next.Players) == 0,
	}
	next.Players = append(next.Players, player)

	events := []Event{
		&PlayerJoinedEvent{
			PlayerID:      player.ID,
			Name:          player.Name,
			Seat:          player.Seat,
			StartingMoney: player.Money,
		},
	}
	return next, events, nil
}

func (r LifecycleRuleset) leave(state *GameState, actor PlayerID) (*GameState, []Event, error) {
	if state.Phase == PhaseEnded {
		return nil, nil, ErrOutOfPhase
	}
	if state.PlayerByID(actor) == nil {
		return nil, nil, ErrPlayerNotInGame
	}

	next := state.Clone()
	var consequences []Event

	if state.Phase == PhaseLobby {
		remaining := make([]Player, 0, len(next.Players)-1)
		hostLeft := false
		for _, p := range next.Players {
			if p.ID == actor {
				hostLeft = p.IsHost
				continue
			}
			remaining = append(remaining, p)
		}
		next.Players = remaining
		if hostLeft && len(remaining) > 0 {
			slices.SortFunc(remaining, func(a, b Player) int { return cmp.Compare(a.Seat, b.Seat) })
			successor := remaining[0].ID
			for i := range next.Players {
				if next.Players[i].ID == successor {
					next.Players[i].IsHost = true
				}
			}
		}
	} else { // PhasePlaying: leaving mid-match eliminates the leaver.
		for i := range next.Players {
			if next.Players[i].ID == actor && next.Players[i].Status == PlayerActive {
				next.Players[i].Status = PlayerEliminated
				consequences = append(consequences, &PlayerEliminatedEvent{
					PlayerID: actor,
					Cause:    "voluntary_leave",
				})
			}
		}
		endEvents, err := maybeEndForInsufficientPlayers(next)
		if err != nil {
			return nil, nil, err
		}
		consequences = append(consequences, endEvents...)
	}

	events := append([]Event{&PlayerLeftEvent{PlayerID: actor, Reason: "voluntary"}}, consequences...)
	return next, events, nil
}

func (r LifecycleRuleset) ready(state *GameState, actor PlayerID, a PlayerReadyAction) (*GameState, []Event, error) {
	if state.Phase != PhaseLobby {
		return nil, nil, ErrOutOfPhase
	}
	player := state.PlayerByID(actor)
	if player == nil {
		return nil, nil, ErrPlayerNotInGame
	}

	next := state.Clone()
	p := next.PlayerByID(actor)
	if p.Ready == a.Ready {
		// No transition, no event: events correspond to actual changes only.
		return next, nil, nil
	}
	p.Ready = a.Ready
	events := []Event{&PlayerReadyChangedEvent{PlayerID: actor, Ready: a.Ready}}
	return next, events, nil
}

func (r LifecycleRuleset) start(state *GameState, cfg *GameConfig, actor PlayerID, rng Rng) (*GameState, []Event, error) {
	if state.Phase != PhaseLobby {
		return nil, nil, ErrOutOfPhase
	}
	host := state.Host()
	if host == nil || host.ID != actor {
		return nil, nil, ErrNotPermitted
	}
	active := state.ActivePlayers()
	if len(active) < cfg.PlayerCount.Min || len(active) > cfg.PlayerCount.Max {
		return nil, nil, ErrGameFull
	}
	for _, p := range active {
		if !p.Ready {
			return nil, nil, ErrNotAllReady
		}
	}

	matchSeed := Split(mustParseSeed(state.RootSeedHex), "match")
	hash, err := cfg.Hash()
	if err != nil {
		return nil, nil, fmt.Errorf("hash config for start: %w", err)
	}

	next := state.Clone()
	if err := next.Phase.canTransitionTo(PhasePlaying); err != nil {
		return nil, nil, err
	}
	next.Phase = PhasePlaying
	next.ConfigHash = hash

	_ = rng // lifecycle transitions consume no randomness; future rulesets use their labeled stream here.

	events := []Event{&GameStartedEvent{ConfigHash: hash, Seed: matchSeed.Hex()}}
	return next, events, nil
}

// maybeEndForInsufficientPlayers ends a playing match when fewer than two
// active players remain.
func maybeEndForInsufficientPlayers(state *GameState) ([]Event, error) {
	if state.Phase != PhasePlaying || len(state.ActivePlayers()) >= 2 {
		return nil, nil
	}
	if err := state.Phase.canTransitionTo(PhaseEnded); err != nil {
		return nil, err
	}
	state.Phase = PhaseEnded
	state.EndReason = "insufficient_players"
	state.WinnerID = nil
	return []Event{&GameEndedEvent{WinnerID: nil, Reason: state.EndReason}}, nil
}

// mustParseSeed is used on seeds the engine itself serialized; failure
// indicates corrupted state rather than user input.
func mustParseSeed(hexed string) Seed {
	s, err := ParseSeed(hexed)
	if err != nil {
		panic(fmt.Sprintf("game: corrupted seed in state: %v", err))
	}
	return s
}
