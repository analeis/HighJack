package game

import "fmt"

// Phase is the high-level lifecycle stage of a match.
type Phase string

const (
	// PhaseLobby: players are joining, readying up, and configuring. The
	// only phase in which the match can be started.
	PhaseLobby Phase = "lobby"
	// PhasePlaying: the match is running under its ruleset.
	PhasePlaying Phase = "playing"
	// PhaseEnded: terminal state; the match accepts no further actions.
	PhaseEnded Phase = "ended"
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseLobby, PhasePlaying, PhaseEnded:
		return true
	default:
		return false
	}
}

// PlayerStatus is a player's participation state within a match.
type PlayerStatus string

const (
	PlayerActive       PlayerStatus = "active"
	PlayerEliminated   PlayerStatus = "eliminated"
	PlayerDisconnected PlayerStatus = "disconnected"
)

func (s PlayerStatus) Valid() bool {
	switch s {
	case PlayerActive, PlayerEliminated, PlayerDisconnected:
		return true
	default:
		return false
	}
}

// transition validates a phase change against the allowed lifecycle.
// The phase graph is intentionally explicit: undocumented transitions do
// not exist.
var allowedTransitions = map[Phase][]Phase{
	PhaseLobby:   {PhasePlaying, PhaseEnded},
	PhasePlaying: {PhaseEnded},
	PhaseEnded:   {},
}

func (p Phase) canTransitionTo(next Phase) error {
	for _, allowed := range allowedTransitions[p] {
		if allowed == next {
			return nil
		}
	}
	return fmt.Errorf("illegal phase transition %q → %q", p, next)
}
