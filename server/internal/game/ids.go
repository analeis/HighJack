// Package game contains the HighJack authoritative game engine boundary.
//
// The package models a HighJack match as a deterministic state machine:
//
//	Action → Validation → State Transition → Events → New State
//
// It depends only on the Go standard library. It knows nothing about HTTP,
// WebSockets, PostgreSQL, authentication, or any frontend concern; the
// server invokes this package directly.
//
// Nothing here implements actual board gameplay yet. What exists today is
// the real, tested skeleton every future system plugs into: phases,
// players, actions, events, configuration validation, canonical hashing,
// and deterministic randomness.
package game

import "fmt"

// PlayerID is an opaque stable identifier for a player within a match.
// The server mints it; clients treat it as a token.
type PlayerID string

// GameID is an opaque identifier for a match/session.
type GameID string

// Seat is the zero-based seat order inside a match. Turn order is seat
// order; seats never change once assigned.
type Seat int

// Money is an amount of in-game currency ("chips"). Money is always an
// integer: fractional amounts are not representable by design, which keeps
// economy arithmetic exact and identical on every platform.
type Money int64

// MaxDisplayNameLen bounds player-chosen display names.
const MaxDisplayNameLen = 32

func (p PlayerID) String() string { return string(p) }

func (m Money) Valid() bool { return m >= 0 }

func validateDisplayName(name string) error {
	n := len([]rune(name))
	if n == 0 {
		return fmt.Errorf("display name must not be empty")
	}
	if n > MaxDisplayNameLen {
		return fmt.Errorf("display name must be at most %d characters", MaxDisplayNameLen)
	}
	return nil
}
