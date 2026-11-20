package game

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// Go's native fuzzing is the fourth tier of engine test maturity. It needs no
// dependency: the engine already depends only on the standard library, and the
// seeded corpus keeps every failure reproducible (GAME_ENGINE.md, "a failure
// always reproduces"). The corpus runs inside the existing test:go gate.
//
// These targets assert the guarantees in GAME_ENGINE.md that had no dedicated
// test: tick discipline, no-op actions emitting nothing, exactly two dice draws
// in fixed order, and a deep Copy with no aliasing. They reuse the structural
// assertions the deterministic suites already make rather than restating them.

// fuzzRoot spreads one fuzzer-provided integer across a full simulation seed.
// Only the first eight bytes vary, so a failure is reproducible from a single
// number in the corpus.
func fuzzRoot(seed uint64) Seed {
	var root Seed
	binary.LittleEndian.PutUint64(root[:8], seed)
	return root
}

// concreteAction builds the action behind an advertised action type. For a
// ready toggle the caller supplies the opposite of the current value; the
// engine rejects a redundant toggle rather than silently ignoring it.
func concreteAction(kind ActionType, ready bool) Action {
	switch kind {
	case ActionPlayerLeave:
		return PlayerLeaveAction{}
	case ActionPlayerReady:
		return PlayerReadyAction{Ready: ready}
	case ActionGameStart:
		return GameStartAction{}
	case ActionRollDice:
		return RollDiceAction{}
	case ActionBuyProperty:
		return BuyPropertyAction{}
	case ActionDeclineBuy:
		return DeclineBuyAction{}
	case ActionEndTurn:
		return EndTurnAction{}
	default:
		return nil
	}
}

// FuzzApplyInvariants drives legal action sequences from arbitrary seeds and
// decision bytes. Every advertised action must be accepted; every accepted one
// must advance exactly one tick, emit events stamped with that tick, and leave
// the board structurally intact. At each step an unadvertised action is probed:
// it must be refused without mutating state.
func FuzzApplyInvariants(f *testing.F) {
	// Corpus seeds include the two seeds used by the deterministic walks
	// (Seed{0x0B} in board_test.go, Seed{0x0A} in lifecycle_test.go) and an
	// odd-shaped decision stream, so historical defect paths stay guarded.
	for _, seed := range []uint64{0x0B, 0x0A, 0x01, 0xFEED, 0x1234_5678} {
		f.Add(seed, []byte{0, 1, 2, 3, 1, 0, 3, 2})
	}
	f.Fuzz(func(t *testing.T, seed uint64, decisions []byte) {
		cfg := DefaultConfig()
		e, state := testMatchEngine(t, cfg, fuzzRoot(seed))
		_, state = startBoardMatch(t, e, state, "A", "B", "C")
		assertBoardInvariants(t, state)

		if len(decisions) == 0 {
			decisions = []byte{0}
		}
		const maxSteps = 240 // bounds -race runtime; a long walk still reaches a terminal phase
		for step := 0; step < maxSteps; step++ {
			if state.Phase.Terminal() {
				return
			}
			actor := currentSeatPlayer(t, state).ID
			legal := LegalActions(state, &cfg, actor)
			if len(legal) == 0 {
				return
			}
			kind := legal[int(decisions[step%len(decisions)])%len(legal)]
			ready := false
			if kind == ActionPlayerReady {
				if p := state.PlayerByID(actor); p != nil {
					ready = !p.Ready
				}
			}
			action := concreteAction(kind, ready)
			if action == nil {
				t.Fatalf("advertised action %s has no concrete form", kind)
			}

			beforeTick := state.Tick
			beforeJSON := mustJSON(t, state)
			next, events, err := e.Apply(state, actor, action)
			if err != nil {
				t.Fatalf("advertised %s rejected at tick %d: %v\nstate: %s", kind, beforeTick, err, beforeJSON)
			}
			if next.Tick != beforeTick+1 {
				t.Fatalf("tick discipline: %s advanced %d → %d", kind, beforeTick, next.Tick)
			}
			if len(events) == 0 {
				t.Fatalf("no-op emitted nothing: accepted %s advanced the tick without events", kind)
			}
			for _, ev := range events {
				if ev.Tick() != next.Tick {
					t.Fatalf("event %T stamped %d, want %d", ev, ev.Tick(), next.Tick)
				}
			}
			assertBoardInvariants(t, next)

			// A refusal must be inert as well as rejected. Probe the first
			// advertised-absent action; LegalActions and Apply use the same
			// predicates, so a disagreement here is a real defect.
			if illegal := firstUnadvertisedAction(state, &cfg, actor); illegal != nil {
				rejected, rejectedEvents, err := e.Apply(state, actor, illegal)
				if err == nil {
					t.Fatalf("unadvertised %s accepted at tick %d", illegal.Type(), beforeTick)
				}
				if rejected != nil || len(rejectedEvents) != 0 {
					t.Fatalf("rejected %s returned state or events", illegal.Type())
				}
				if after := mustJSON(t, state); after != beforeJSON {
					t.Fatalf("rejected %s mutated state", illegal.Type())
				}
			}
			state = next
		}
	})
}

func firstUnadvertisedAction(state *GameState, cfg *GameConfig, actor PlayerID) Action {
	for _, kind := range AllActionTypes() {
		// Joining is keyed on an empty actor and a display name, not on this
		// player; it is covered by the lifecycle suite.
		if kind == ActionPlayerJoin {
			continue
		}
		action := concreteAction(kind, true)
		if IsLegal(state, cfg, actor, action) {
			continue
		}
		return action
	}
	return nil
}

// FuzzRollConsumesTwoDraws checks the "no hidden randomness" guarantee: a roll
// uses the per-tick stream and takes exactly its first two draws, in order,
// whatever the die values are. Since Apply constructs that stream internally,
// the observable contract is that the dice equal those first two draws.
func FuzzRollConsumesTwoDraws(f *testing.F) {
	for _, seed := range []uint64{0x0B, 0x0A, 0x01, 0xFEED, 0x1234_5678, 0xFFFF_FFFF} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		cfg := DefaultConfig()
		root := fuzzRoot(seed)
		e, state := testMatchEngine(t, cfg, root)
		_, state = startBoardMatch(t, e, state, "A", "B", "C")
		actor := currentSeatPlayer(t, state).ID
		tick := state.Tick

		rng := NewSeededRng(Split(root, fmt.Sprintf("tick:%d", tick)))
		want1, want2 := rng.IntN(6)+1, rng.IntN(6)+1
		next, events := mustApply(t, e, state, actor, RollDiceAction{})
		roll := rollEvent(t, events)
		if roll.Die1 != want1 || roll.Die2 != want2 {
			t.Fatalf(
				"roll at tick %d = (%d,%d), want the tick stream's first two draws (%d,%d)",
				tick, roll.Die1, roll.Die2, want1, want2,
			)
		}
		if next.Tick != tick+1 {
			t.Fatalf("roll advanced tick %d → %d", tick, next.Tick)
		}
	})
}

// FuzzCloneDoesNotAlias checks the value-typed ownership guarantee against
// arbitrary seeds: mutating a clone's ownership, balance, turn or winner must
// not reach the original. The WinnerID pointer was the historical leak; a
// space owner was stored by value from the start and is the control case.
func FuzzCloneDoesNotAlias(f *testing.F) {
	for _, seed := range []uint64{0x0B, 0x0A, 0x01, 0xFEED} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		cfg := DefaultConfig()
		e, state := testMatchEngine(t, cfg, fuzzRoot(seed))
		ids, state := startBoardMatch(t, e, state, "A", "B", "C")
		if len(state.Board.Spaces) == 0 || len(ids) < 2 {
			t.Fatal("default match has no spaces or too few players")
		}
		idx := int(seed % uint64(len(state.Board.Spaces)))
		state.Board.Spaces[idx].Owned = true
		state.Board.Spaces[idx].Owner = ids[0]
		winner := ids[0]
		state.WinnerID = &winner

		wantOwner := state.Board.Spaces[idx].Owner
		wantMoney := state.Players[0].Money
		wantPhase := state.Turn.Phase
		wantWinner := *state.WinnerID

		clone := state.Clone()
		clone.Board.Spaces[idx].Owner = ids[1]
		clone.Players[0].Money = 0
		clone.Turn.Phase = TurnOver
		*clone.WinnerID = ids[1]

		if got := state.Board.Spaces[idx].Owner; got != wantOwner {
			t.Fatalf("clone aliased space owner: %s → %s", wantOwner, got)
		}
		if got := state.Players[0].Money; got != wantMoney {
			t.Fatalf("clone aliased player money: %d → %d", wantMoney, got)
		}
		if got := state.Turn.Phase; got != wantPhase {
			t.Fatalf("clone aliased turn phase: %s → %s", wantPhase, got)
		}
		if got := *state.WinnerID; got != wantWinner {
			t.Fatalf("clone aliased winner: %s → %s", wantWinner, got)
		}
	})
}
