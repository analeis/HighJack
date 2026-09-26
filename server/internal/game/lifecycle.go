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

	rootSeed, err := parseStateSeed(state)
	if err != nil {
		return nil, nil, err
	}
	matchSeed := Split(rootSeed, "match")
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
	// Every terminal phase, not just Ended. PhaseInterrupted is terminal and is
	// never resumed, so mutating a roster in it would edit a match that is
	// already over.
	if state.Phase.Terminal() {
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
				// Holdings revert to the bank on every elimination, not just on
				// bankruptcy. A player who leaves while owning property would
				// otherwise leave it permanently owned by an eliminated player:
				// unpurchasable, because buy and decline both reject an owned
				// space, and rent-free, because a payer skips an inactive owner.
				released := releaseHoldings(next, actor)
				next.Players[i].Status = PlayerEliminated
				consequences = append(consequences,
					&PlayerBankruptEvent{
						PlayerID:       actor,
						Cause:          "voluntary_leave",
						ReleasedSpaces: released,
					},
					&PlayerEliminatedEvent{
						PlayerID: actor,
						Cause:    "voluntary_leave",
					},
				)
			}
		}
		endEvents, err := maybeEndForInsufficientPlayers(next)
		if err != nil {
			return nil, nil, err
		}
		consequences = append(consequences, endEvents...)
		if next.Phase == PhasePlaying {
			// The match continues: a leaver holding the current turn must
			// not strand it. Advance past them deterministically.
			leaver := state.PlayerByID(actor)
			if leaver != nil && leaver.Seat == next.Turn.CurrentSeat {
				seat, round := advanceSeat(next, leaver.Seat)
				next.Turn.CurrentSeat = seat
				next.Turn.Round = round
				next.Turn.Phase = TurnAwaitRoll
				next.Turn.DoublesStreak = 0
				next.Turn.AwardExtraRoll = false
				consequences = append(consequences, &TurnAdvancedEvent{Seat: seat, Round: round})
			}
		}
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
	if len(active) > cfg.PlayerCount.Max {
		return nil, nil, ErrGameFull
	}
	if len(active) < cfg.PlayerCount.Min {
		return nil, nil, ErrNotEnoughPlayers
	}
	for _, p := range active {
		if !p.Ready {
			return nil, nil, ErrNotAllReady
		}
	}

	rootSeed, err := parseStateSeed(state)
	if err != nil {
		return nil, nil, err
	}
	matchSeed := Split(rootSeed, "match")
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

	// The board is derived from validated config and already exists from
	// match creation; starting the match only arms the first turn. Players
	// stand on space 0 and the lowest active seat rolls first in round 1.
	if len(next.Board.Spaces) == 0 {
		next.Board = newBoardRuntime(cfg)
	}
	next.Turn = TurnState{CurrentSeat: active[0].Seat, Phase: TurnAwaitRoll, Round: 1}

	_ = rng // lifecycle transitions consume no randomness; board rolls use their per-tick stream.

	events := []Event{&GameStartedEvent{ConfigHash: hash, Seed: matchSeed.Hex()}}
	return next, events, nil
}

// maybeEndForInsufficientPlayers ends a playing match when fewer than two
// active players remain. A sole survivor wins by last_standing; an empty
// table ends with no winner. Shared by voluntary leave and bankruptcy so
// both paths record identical terminal metadata.
func maybeEndForInsufficientPlayers(state *GameState) ([]Event, error) {
	active := state.ActivePlayers()
	if state.Phase != PhasePlaying || len(active) >= 2 {
		return nil, nil
	}
	if err := state.Phase.canTransitionTo(PhaseEnded); err != nil {
		return nil, err
	}
	state.Phase = PhaseEnded
	if len(active) == 1 {
		winner := active[0].ID
		state.WinnerID = &winner
		state.EndReason = "last_standing"
	} else {
		state.WinnerID = nil
		state.EndReason = "insufficient_players"
	}
	return []Event{&GameEndedEvent{WinnerID: state.WinnerID, Reason: state.EndReason}}, nil
}

// parseStateSeed reads the root seed out of a state.
//
// This returns an error rather than panicking. The field is optional on the
// wire, so a snapshot written by a build that predates it unmarshals
// successfully and would then panic on the next join or start — in the middle of
// a request, on the goroutine that holds the match lock, which wedges the match
// permanently. Corrupt input must be an error the caller can report.
func parseStateSeed(state *GameState) (Seed, error) {
	s, err := ParseSeed(state.RootSeedHex)
	if err != nil {
		return Seed{}, fmt.Errorf("game: state has no usable root seed: %w", err)
	}
	return s, nil
}
