package game

import (
	"cmp"
	"slices"
)

// Player is a participant in a match.
type Player struct {
	ID     PlayerID     `json:"playerId"`
	Name   string       `json:"name"`
	Seat   Seat         `json:"seat"`
	Money  Money        `json:"money"`
	Status PlayerStatus `json:"status"`
	Ready  bool         `json:"ready"`
	IsHost bool         `json:"isHost"`
}

// Active reports whether the player can act right now.
func (p *Player) Active() bool { return p.Status == PlayerActive }

// GameState is the complete authoritative state of one match at a point in
// time. It is a plain serializable value: nothing inside it references the
// transport layer, storage, or wall-clock time (wall-clock bookkeeping is a
// server concern, not simulation state).
//
// Tick counts every applied transition since match creation and defines the
// total causal order of events.
//
// Engine handlers must treat state as immutable input and return a new,
// modified copy; they must never mutate the caller's value in place.
type GameState struct {
	ID      GameID   `json:"id"`
	Phase   Phase    `json:"phase"`
	Tick    uint64   `json:"tick"`
	Players []Player `json:"players"`

	// RootSeedHex records the engine root seed for replay/debugging. It is
	// server-visible metadata, not part of client-visible game state; the
	// API layer strips it before sending state to clients.
	RootSeedHex string `json:"rootSeedHex,omitempty"`

	// WinnerID is set exactly once when the match ends.
	WinnerID   *PlayerID `json:"winnerId,omitempty"`
	EndReason  string    `json:"endReason,omitempty"`
	ConfigHash string    `json:"configHash,omitempty"`
}

// Clone returns a deep copy of the state so handlers can modify freely.
func (s *GameState) Clone() *GameState {
	out := *s
	if s.Players != nil {
		out.Players = make([]Player, len(s.Players))
		copy(out.Players, s.Players)
	}
	return &out
}

// PlayerByID returns the player with the given id, or nil.
func (s *GameState) PlayerByID(id PlayerID) *Player {
	for i := range s.Players {
		if s.Players[i].ID == id {
			return &s.Players[i]
		}
	}
	return nil
}

// Host returns the current host player, or nil if there is none.
func (s *GameState) Host() *Player {
	for i := range s.Players {
		if s.Players[i].IsHost {
			return &s.Players[i]
		}
	}
	return nil
}

// ActivePlayers returns active players sorted by seat order.
func (s *GameState) ActivePlayers() []Player {
	var out []Player
	for _, p := range s.Players {
		if p.Active() {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Player) int { return cmp.Compare(a.Seat, b.Seat) })
	return out
}

// NextSeat returns the lowest unused seat number.
func (s *GameState) NextSeat() Seat {
	used := make(map[Seat]bool, len(s.Players))
	for _, p := range s.Players {
		used[p.Seat] = true
	}
	for seat := Seat(0); ; seat++ {
		if !used[seat] {
			return seat
		}
	}
}
