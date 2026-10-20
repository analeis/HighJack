package match

import (
	"context"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
)

// This file measures the match runtime's hot path.
//
// The retained event window is trimmed on every accepted action, and the trim
// used to allocate a fresh slice and copy the whole window into it. With a window
// of 1024 envelopes that is roughly 24 kilobytes of garbage per action, on the
// path every player action takes.
//
// The benchmark exists so the fix is chosen from a measurement rather than from
// an argument about what ought to be slow.

// benchMatch builds a match that is already playing, with the history window
// full, so a benchmarked action pays the trim cost every time.
func benchMatch(b *testing.B) *fixture {
	b.Helper()
	f := newFixture(b)
	f.playing(b)
	// Fill the window past its cap. The trim keeps it at the cap, so once the
	// window is full every subsequent action trims.
	driveMatch(b, f, 1500)
	if f.m.base == 0 {
		b.Fatal("the history window never filled, so the trim is not being exercised")
	}
	return f
}

// BenchmarkApplyActionWithFullHistory measures a whole accepted action with the
// window full, which is the path every player action takes in a long match.
//
// The match is driven to a full window first; the benchmark then applies
// actions, each of which trims.
func BenchmarkApplyActionWithFullHistory(b *testing.B) {
	f := benchMatch(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := f.m.Snapshot()
		session := "s1"
		if snap.Turn.CurrentSeat == 1 {
			session = "s2"
		}
		if _, err := f.m.ApplyAction(ctx, session, int64(1_000_000+i), game.RollDiceAction{}); err != nil {
			b.Fatalf("action %d: %v", i, err)
		}
	}
}

// BenchmarkCommitTransitionWithFullHistory isolates the commit path — the
// allocation the history trim makes — from the rest of the action.
//
// It drives the state machine directly rather than playing a match, so it is
// deterministic: a real match ends, and a benchmark that cannot run to
// completion measures nothing.
func BenchmarkCommitTransitionWithFullHistory(b *testing.B) {
	m := &Match{log: testLogger(), state: &game.GameState{}}
	// A window already at its cap, with each tick carrying several events, which
	// is what a transition produces.
	for tick := uint64(1); tick <= 400; tick++ {
		for i := 0; i < 3; i++ {
			m.history = append(m.history, EventEnvelope{Tick: tick})
		}
	}
	if uint64(len(m.history)) <= historyCap {
		b.Fatalf("the window holds %d events, which is not above the %d cap", len(m.history), historyCap)
	}
	event := &game.DiceRolledEvent{PlayerID: "pl_x", Die1: 3, Die2: 4}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.commitLocked([]game.Event{event}, m.state)
	}
}
