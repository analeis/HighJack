package game

import (
	"encoding/json"
	"testing"
)

// testEngine builds an engine with a fixed root seed so every test in this
// file is fully deterministic.
func testEngine(t *testing.T, cfg GameConfig) (*Engine, *GameState) {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	root := Seed{0x0A}
	e, err := NewEngine(&cfg, root, LifecycleRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	return e, e.NewState("game_test")
}

func mustApply(t *testing.T, e *Engine, state *GameState, actor PlayerID, action Action) (*GameState, []Event) {
	t.Helper()
	next, events, err := e.Apply(state, actor, action)
	if err != nil {
		t.Fatalf("Apply(%s): %v", action.Type(), err)
	}
	return next, events
}

func joinAndReady(t *testing.T, e *Engine, state *GameState, names ...string) ([]PlayerID, *GameState) {
	t.Helper()
	var ids []PlayerID
	for _, name := range names {
		var events []Event
		state, events = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: name})
		joined := events[0].(*PlayerJoinedEvent)
		ids = append(ids, joined.PlayerID)
	}
	for _, id := range ids {
		state, _ = mustApply(t, e, state, id, PlayerReadyAction{Ready: true})
	}
	return ids, state
}

func TestJoinEmitsPlayerJoinedWithSeatMoneyAndHost(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())

	next, events := mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "Ace"})

	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	joined, ok := events[0].(*PlayerJoinedEvent)
	if !ok {
		t.Fatalf("expected PlayerJoinedEvent, got %T", events[0])
	}
	if joined.Name != "Ace" || joined.Seat != 0 || joined.StartingMoney != 1500 {
		t.Fatalf("unexpected join payload: %+v", joined)
	}
	p := next.PlayerByID(joined.PlayerID)
	if p == nil || !p.IsHost || p.Status != PlayerActive || !p.Active() {
		t.Fatalf("first player should be active host, got %+v", p)
	}
	if next.Tick != 1 {
		t.Fatalf("tick should advance exactly once, got %d", next.Tick)
	}
}

func TestJoinsAssignSequentialSeats(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, _ := joinAndReady(t, e, state, "A", "B", "C")
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(ids))
	}
	// Deterministic ids: same seed + seat ⇒ same id.
	again := DeriveMatchPlayerID(Split(Seed{0x0A}, "match"), 1)
	if ids[1] != again {
		t.Fatalf("player id derivation not deterministic: %s != %s", ids[1], again)
	}
}

func TestJoinRejectedWhenFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlayerCount = PlayerCountRange{Min: 2, Max: 2}
	e, state := testEngine(t, cfg)

	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "A"})
	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "B"})

	if _, _, err := e.Apply(state, "", PlayerJoinAction{DisplayName: "C"}); err != ErrGameFull {
		t.Fatalf("expected ErrGameFull, got %v", err)
	}
}

func TestInvalidActionLeavesStateUntouched(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlayerCount = PlayerCountRange{Min: 2, Max: 2}
	e, state := testEngine(t, cfg)
	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "A"})
	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "B"})
	before := mustJSON(t, state)

	if _, _, err := e.Apply(state, "", PlayerJoinAction{DisplayName: "C"}); err == nil {
		t.Fatal("expected rejection")
	}
	after := mustJSON(t, state)
	if before != after {
		t.Fatalf("invalid action mutated state:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestStartRequiresHostAllReadyAndWithinRange(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, state := joinAndReady(t, e, state, "A", "B")

	// Non-host cannot start.
	if _, _, err := e.Apply(state, ids[1], GameStartAction{}); err != ErrNotPermitted {
		t.Fatalf("expected ErrNotPermitted for non-host, got %v", err)
	}

	// Un-ready a player; start must now fail.
	state, _ = mustApply(t, e, state, ids[1], PlayerReadyAction{Ready: false})
	if _, _, err := e.Apply(state, ids[0], GameStartAction{}); err != ErrNotAllReady {
		t.Fatalf("expected ErrNotAllReady, got %v", err)
	}
	state, _ = mustApply(t, e, state, ids[1], PlayerReadyAction{Ready: true})

	next, events := mustApply(t, e, state, ids[0], GameStartAction{})
	if next.Phase != PhasePlaying {
		t.Fatalf("phase should be playing, got %q", next.Phase)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %+v", events)
	}
	started := events[0].(*GameStartedEvent)
	hash, err := e.Config().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if started.ConfigHash != hash {
		t.Fatalf("config hash mismatch: %s != %s", started.ConfigHash, hash)
	}
	if want := Split(Seed{0x0A}, "match").Hex(); started.Seed != want {
		t.Fatalf("seed mismatch: %s != %s", started.Seed, want)
	}
}

func TestReadyChangeWithoutDifferenceEmitsNothing(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, state := joinAndReady(t, e, state, "A")
	tickBefore := state.Tick
	state, events := mustApply(t, e, state, ids[0], PlayerReadyAction{Ready: true})
	if len(events) != 0 {
		t.Fatalf("no-op ready toggle must emit no events, got %+v", events)
	}
	if state.Tick != tickBefore+1 {
		t.Fatalf("tick must advance exactly once per applied action: %d → %d", tickBefore, state.Tick)
	}
}

func TestLeaveInLobbyRemovesPlayerAndReassignsHost(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, state := joinAndReady(t, e, state, "A", "B")

	next, events := mustApply(t, e, state, ids[0], PlayerLeaveAction{})

	left := events[0].(*PlayerLeftEvent)
	if left.Reason != "voluntary" {
		t.Fatalf("unexpected reason %q", left.Reason)
	}
	if len(next.Players) != 1 {
		t.Fatalf("expected 1 remaining player, got %d", len(next.Players))
	}
	if !next.Players[0].IsHost {
		t.Fatal("host should pass to the remaining lowest-seat player")
	}
	if next.PlayerByID(ids[0]) != nil {
		t.Fatal("leaver should be gone from lobby state")
	}
}

func TestLeaveDuringPlayEliminatesAndEndsWhenLonely(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, state := joinAndReady(t, e, state, "A", "B")
	state, _ = mustApply(t, e, state, ids[0], GameStartAction{})

	next, events := mustApply(t, e, state, ids[0], PlayerLeaveAction{})

	if next.Phase != PhaseEnded {
		t.Fatalf("match with <2 active players must end, got phase %q", next.Phase)
	}
	if next.EndReason != "insufficient_players" || next.WinnerID != nil {
		t.Fatalf("unexpected end metadata: %+v", next)
	}
	var sawEliminated, sawEnded bool
	for _, ev := range events {
		switch e := ev.(type) {
		case *PlayerEliminatedEvent:
			sawEliminated = true
			if e.Cause != "voluntary_leave" {
				t.Fatalf("unexpected cause %q", e.Cause)
			}
			if next.PlayerByID(e.PlayerID).Status != PlayerEliminated {
				t.Fatal("leaver must be eliminated in state")
			}
		case *GameEndedEvent:
			sawEnded = true
		}
	}
	if !sawEliminated || !sawEnded {
		t.Fatalf("missing elimination/end events: %+v", events)
	}
}

func TestDeterministicSimulationSameInputSameOutput(t *testing.T) {
	run := func() (string, []string) {
		e, state := testEngine(t, DefaultConfig())
		var eventLog []string

		for _, name := range []string{"Ace", "Bit", "Cy"} {
			var events []Event
			state, events = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: name})
			eventLog = append(eventLog, renderEvents(events)...)
		}

		// A rejected action must leave no trace in the log or the state.
		if _, _, err := e.Apply(state, "", PlayerJoinAction{DisplayName: ""}); err == nil {
			t.Fatal("expected empty-name join to be rejected")
		}

		players := state.ActivePlayers()
		for _, id := range players {
			var events []Event
			state, events = mustApply(t, e, state, id.ID, PlayerReadyAction{Ready: true})
			eventLog = append(eventLog, renderEvents(events)...)
		}
		state, events := mustApply(t, e, state, players[0].ID, GameStartAction{})
		eventLog = append(eventLog, renderEvents(events)...)
		return mustJSON(t, state), eventLog
	}

	stateA, logA := run()
	stateB, logB := run()

	if stateA != stateB {
		t.Fatalf("same inputs produced different states:\n%s\n---\n%s", stateA, stateB)
	}
	for i := range logA {
		if logA[i] != logB[i] {
			t.Fatalf("event logs diverged at %d:\n%s\n%s", i, logA[i], logB[i])
		}
	}
}

func TestPhaseTransitionsAreGuarded(t *testing.T) {
	e, state := testEngine(t, DefaultConfig())
	ids, state := joinAndReady(t, e, state, "A", "B")
	started, _ := mustApply(t, e, state, ids[0], GameStartAction{})

	// No joining after start.
	if _, _, err := e.Apply(started, "", PlayerJoinAction{DisplayName: "Late"}); err != ErrAlreadyStarted {
		t.Fatalf("expected ErrAlreadyStarted, got %v", err)
	}
	// Ended is terminal.
	ended := *started
	ended.Phase = PhaseEnded
	if _, _, err := e.Apply(&ended, ids[0], PlayerReadyAction{Ready: true}); err != ErrOutOfPhase {
		t.Fatalf("expected ErrOutOfPhase on ended match, got %v", err)
	}
}

// ---- helpers ---------------------------------------------------------------

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func renderEvents(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		out = append(out, string(data))
	}
	return out
}
