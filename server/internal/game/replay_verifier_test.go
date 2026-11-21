package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// Replay verifier: the engine deliberately retains a root seed, seed-derived
// ids and a durable event payload containing the match seed (see marshal.go,
// rng.go DeriveMatchSeed and event_encode_test.go). Nothing consumed them —
// until now. These tests prove the retained data is sufficient to regenerate
// both the event log and the final state from genesis.
//
// The real log lives in PostgreSQL behind pgx and is read by the gated
// persistence suite. This verifier works on the durable *format* (MarshalEvent
// payloads, tick/seq/kind rows and a MarshalGameState snapshot), which is the
// algorithm the docs promise: "event logs re-applied to genesis state must
// reproduce final state hashes" (TESTING.md). It is deliberately stdlib-only.
//
// What makes this different from TestDeterministicSimulationSameInputSameOutput
// and TestBoardDeterminism: those build actions in test code. Here the actions
// are reconstructed from the persisted events, so a field that the log failed
// to retain would make reproduction impossible.

// durableEvent mirrors one row of match_events: (tick, seq, kind, payload).
// seq is the index within a transition, so seq 0 is its primary event — the
// one that names the action; later events are consequences.
type durableEvent struct {
	tick    uint64
	seq     int
	kind    EventType
	payload []byte
}

// The recording and the replay must use the same match id: it is part of the
// durable state snapshot that gets compared byte-for-byte.
const replayMatchID = "game_board_test"

// recordScriptedMatch applies a scripted match and captures its durable log
// the way persistence does, plus the final state snapshot. The actor of each
// action is used only while recording; the verifier must recover it from the
// log and the state, as a real replay would.
func recordScriptedMatch(t *testing.T, root Seed, cfg *GameConfig) ([]durableEvent, []byte) {
	t.Helper()
	e, err := NewEngine(cfg, root, MatchRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	state := e.NewState(replayMatchID)
	var log []durableEvent
	var ids []PlayerID
	apply := func(actor PlayerID, action Action) {
		t.Helper()
		var events []Event
		state, events = mustApply(t, e, state, actor, action)
		for i, ev := range events {
			payload, err := MarshalEvent(ev)
			if err != nil {
				t.Fatal(err)
			}
			log = append(log, durableEvent{
				tick: ev.Tick(), seq: i, kind: ev.Type(), payload: payload,
			})
			if joined, ok := ev.(*PlayerJoinedEvent); ok {
				ids = append(ids, joined.PlayerID)
			}
		}
	}

	// Join, ready and start. Join carries an empty actor and start's actor is
	// not in its payload — both have to be recovered during replay.
	apply("", PlayerJoinAction{DisplayName: "A"})
	apply("", PlayerJoinAction{DisplayName: "B"})
	if len(ids) != 2 {
		t.Fatalf("expected two joined ids, got %d", len(ids))
	}
	apply(ids[0], PlayerReadyAction{Ready: true})
	apply(ids[1], PlayerReadyAction{Ready: true})
	apply(ids[0], GameStartAction{}) // first joiner is host

	// A few rounds of the board loop, declining every buy. Covers rolls,
	// declines, turn advances and their consequence events.
	for round := 0; round < 3 && state.Phase == PhasePlaying; round++ {
		for _, id := range ids {
			p := state.PlayerByID(id)
			if state.Phase != PhasePlaying || p == nil || !p.Active() || p.Seat != state.Turn.CurrentSeat {
				continue
			}
			apply(id, RollDiceAction{})
			if state.Phase != PhasePlaying {
				break
			}
			if state.Turn.Phase == TurnAwaitBuyDecision {
				apply(id, DeclineBuyAction{})
			}
			if state.Phase == PhasePlaying && state.Turn.Phase == TurnOver && state.Turn.CurrentSeat == p.Seat {
				apply(id, EndTurnAction{})
			}
		}
	}

	final, err := MarshalGameState(state)
	if err != nil {
		t.Fatal(err)
	}
	return log, final
}

// actionFromDurable recovers the action behind a transition's primary event.
// For join the actor is empty; for start and end turn the actor is not in the
// event payload and is derived from the state at replay time — exactly the
// information a verifier has. All other events carry the acting PlayerID.
func actionFromDurable(ev durableEvent, state *GameState) (PlayerID, Action, error) {
	switch ev.kind {
	case EventPlayerJoined:
		var x PlayerJoinedEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return "", PlayerJoinAction{DisplayName: x.Name}, nil
	case EventPlayerLeft:
		var x PlayerLeftEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return x.PlayerID, PlayerLeaveAction{}, nil
	case EventPlayerReadyChanged:
		var x PlayerReadyChangedEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return x.PlayerID, PlayerReadyAction{Ready: x.Ready}, nil
	case EventGameStarted:
		host := state.Host()
		if host == nil {
			return "", nil, errors.New("replay: game_started with no host")
		}
		return host.ID, GameStartAction{}, nil
	case EventDiceRolled:
		var x DiceRolledEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return x.PlayerID, RollDiceAction{}, nil
	case EventPropertyBought:
		var x PropertyBoughtEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return x.PlayerID, BuyPropertyAction{}, nil
	case EventBuyDeclined:
		var x BuyDeclinedEvent
		if err := json.Unmarshal(ev.payload, &x); err != nil {
			return "", nil, err
		}
		return x.PlayerID, DeclineBuyAction{}, nil
	case EventTurnAdvanced:
		for _, p := range state.Players {
			if p.Active() && p.Seat == state.Turn.CurrentSeat {
				return p.ID, EndTurnAction{}, nil
			}
		}
		return "", nil, errors.New("replay: turn_advanced with no current-seat player")
	default:
		return "", nil, fmt.Errorf("replay: %s is not a primary action event", ev.kind)
	}
}

// verifyReplay rebuilds the match from genesis using only the durable rows and
// the root seed. It returns an error on the first divergence, so a caller can
// assert both reproduction (no error) and tamper detection (an error).
func verifyReplay(rows []durableEvent, root Seed, cfg *GameConfig, expectedFinal []byte) error {
	e, err := NewEngine(cfg, root, MatchRuleset{})
	if err != nil {
		return err
	}
	state := e.NewState(replayMatchID)

	for i := 0; i < len(rows); {
		tick := rows[i].tick
		var group []durableEvent
		for i < len(rows) && rows[i].tick == tick {
			group = append(group, rows[i])
			i++
		}
		if len(group) == 0 || group[0].seq != 0 {
			return fmt.Errorf("replay: tick %d has no primary (seq 0) event", tick)
		}
		actor, action, err := actionFromDurable(group[0], state)
		if err != nil {
			return err
		}
		next, events, err := e.Apply(state, actor, action)
		if err != nil {
			return fmt.Errorf("replay: apply %s at tick %d: %w", group[0].kind, tick, err)
		}
		if len(events) != len(group) {
			return fmt.Errorf("replay: tick %d produced %d events, recorded %d", tick, len(events), len(group))
		}
		for j, ev := range events {
			payload, err := MarshalEvent(ev)
			if err != nil {
				return err
			}
			if ev.Type() != group[j].kind {
				return fmt.Errorf("replay: tick %d seq %d kind %s != recorded %s", tick, j, ev.Type(), group[j].kind)
			}
			if ev.Tick() != group[j].tick {
				return fmt.Errorf("replay: tick %d seq %d stamped %d != recorded %d", tick, j, ev.Tick(), group[j].tick)
			}
			if !bytes.Equal(payload, group[j].payload) {
				return fmt.Errorf(
					"replay: tick %d seq %d payload mismatch\n recorded: %s\n replayed: %s",
					tick, j, group[j].payload, payload,
				)
			}
		}
		state = next
	}

	final, err := MarshalGameState(state)
	if err != nil {
		return err
	}
	if !bytes.Equal(final, expectedFinal) {
		return fmt.Errorf("replay: final state mismatch\n recorded: %s\n replayed: %s", expectedFinal, final)
	}
	return nil
}

func TestReplayVerifierReproducesFinalStateAndEventLog(t *testing.T) {
	root := Seed{0x0B}
	cfg := DefaultConfig()
	rows, final := recordScriptedMatch(t, root, &cfg)
	if len(rows) == 0 {
		t.Fatal("scripted match produced no durable events")
	}
	if err := verifyReplay(rows, root, &cfg, final); err != nil {
		t.Fatalf("retained data was not sufficient to replay the match: %v", err)
	}
}

func TestReplayVerifierDetectsTamperedEvent(t *testing.T) {
	root := Seed{0x0B}
	cfg := DefaultConfig()
	rows, final := recordScriptedMatch(t, root, &cfg)

	// Tamper with an event payload the verifier relies on. Change the acting
	// player id of a primary event: reconstruction would pick the wrong actor
	// (or none), so a verifier that ignored the payload would still pass.
	var target *durableEvent
	for i := range rows {
		if rows[i].seq == 0 && rows[i].kind == EventGameStarted {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		t.Fatal("scripted match has no game_started event to tamper with")
	}
	var started GameStartedEvent
	if err := json.Unmarshal(target.payload, &started); err != nil {
		t.Fatal(err)
	}
	started.ConfigHash = "tampered-hash"
	tampered, err := json.Marshal(started)
	if err != nil {
		t.Fatal(err)
	}
	original := target.payload
	target.payload = tampered
	defer func() { target.payload = original }()

	if err := verifyReplay(rows, root, &cfg, final); err == nil {
		t.Fatal("verifier accepted a tampered event log")
	}
}
