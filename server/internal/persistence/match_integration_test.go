package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/game"
)

// These tests exercise the match repository against a real PostgreSQL:
// JSONB round-trips, transaction atomicity, token resolution, and the
// interrupted-match policy. An in-memory fake could not prove any of it.

func matchFixture(t *testing.T) (*Store, game.GameConfig, *game.GameState, game.GameID) {
	t.Helper()
	store := integrationDB(t)
	cfg := game.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture config invalid: %v", err)
	}
	engine, err := game.NewEngine(&cfg, game.Seed{0x0C}, game.MatchRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	id := game.GameID("m_test_" + t.Name())
	state := engine.NewState(id)
	state.Board = game.NewBoardState(&cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.CreateMatch(ctx, id, &cfg, state, "00"); err != nil {
		t.Fatalf("create match: %v", err)
	}
	t.Cleanup(func() { cleanupMatch(t, store, id) })
	return store, cfg, state, id
}

func cleanupMatch(t *testing.T, store *Store, id game.GameID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Snapshots and events cascade from the games row.
	_, _ = store.Pool().Exec(ctx, `DELETE FROM games WHERE id = $1`, string(id))
	_, _ = store.Pool().Exec(ctx, `DELETE FROM game_configs WHERE id = $1`, string(id)+":config")
}

func TestCreateAndLoadMatchRoundTrips(t *testing.T) {
	store, cfg, state, id := matchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	row, err := store.LoadMatch(ctx, id)
	if err != nil {
		t.Fatalf("load match: %v", err)
	}
	if row.ConfigHash == "" || row.SeedHex != "00" {
		t.Fatalf("unexpected metadata: %+v", row)
	}
	// Config survives the JSONB round trip byte-identically.
	wantCfg, err := cfg.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	gotCfg, err := row.Config.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCfg) != string(wantCfg) {
		t.Fatalf("config diverged after round trip:\n got: %s\nwant: %s", gotCfg, wantCfg)
	}
	if len(row.State.Board.Spaces) != len(state.Board.Spaces) {
		t.Fatalf("board spaces lost: %d vs %d", len(row.State.Board.Spaces), len(state.Board.Spaces))
	}
}

func TestLoadMatchMissingReturnsErrNoMatch(t *testing.T) {
	store := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := store.LoadMatch(ctx, "m_does_not_exist"); err != ErrNoMatch {
		t.Fatalf("expected ErrNoMatch, got %v", err)
	}
}

// TestCommitTransitionPersistsStateAndEvents proves the durability promise:
// one transaction writes the new state, its events, and the cursor.
func TestCommitTransitionPersistsStateAndEvents(t *testing.T) {
	store, _, state, id := matchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events := []game.Event{
		&game.BankTransferEvent{PlayerID: "pl_x", Amount: 50, Direction: "to_player", Reason: "pass_go"},
	}
	next := state.Clone()
	next.Tick = state.Tick + 1
	next.Phase = game.PhasePlaying
	next.Turn = game.TurnState{CurrentSeat: 0, Phase: game.TurnAwaitRoll, Round: 1}

	if err := store.CommitTransition(ctx, id, next, events); err != nil {
		t.Fatalf("commit: %v", err)
	}

	row, err := store.LoadMatch(ctx, id)
	if err != nil {
		t.Fatalf("load after commit: %v", err)
	}
	if row.Cursor != next.Tick {
		t.Fatalf("cursor = %d, want %d", row.Cursor, next.Tick)
	}
	if row.State.Phase != game.PhasePlaying {
		t.Fatalf("phase not persisted: %q", row.State.Phase)
	}
	var n int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM match_events WHERE match_id = $1`, string(id)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(events) {
		t.Fatalf("events persisted = %d, want %d", n, len(events))
	}
}

// TestCommitTransitionRollsBackOnFailure proves a failed transaction leaves
// no partial state: a bad event payload must not advance the cursor.
func TestCommitTransitionRollsBackOnFailure(t *testing.T) {
	store, _, state, id := matchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	before, err := store.LoadMatch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	// Force a failure inside the transaction after the event inserts: the
	// turn_phase check rejects an unknown phase. The events are written
	// first, so a correct implementation must roll them back too.
	bad := []game.Event{&game.TurnAdvancedEvent{Seat: 1, Round: 1}}
	next := state.Clone()
	next.Tick = state.Tick + 1
	next.Phase = game.PhasePlaying
	next.Turn = game.TurnState{CurrentSeat: 0, Phase: game.TurnPhase("not_a_phase"), Round: 1}
	if err := store.CommitTransition(ctx, id, next, bad); err == nil {
		t.Fatal("expected the constraint violation to fail the commit")
	}

	after, err := store.LoadMatch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cursor != before.Cursor || after.State.Tick != before.State.Tick {
		t.Fatalf("failed commit advanced state: cursor %d→%d, tick %d→%d",
			before.Cursor, after.Cursor, before.State.Tick, after.State.Tick)
	}
	// The event written before the failure must not survive either.
	var n int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM match_events WHERE match_id = $1`, string(id)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("failed commit left %d event(s) behind; state and log diverged", n)
	}
}

func TestPlayerTokenResolvesOnlyWithinItsMatch(t *testing.T) {
	store, _, _, id := matchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p := game.Player{ID: "pl_token_test", Name: "Ace", Seat: 0, Money: 1500, Status: game.PlayerActive}
	const tokenHash = "hash-abc-123"
	if err := store.SavePlayer(ctx, id, p, tokenHash); err != nil {
		t.Fatalf("save player: %v", err)
	}
	got, err := store.PlayerByTokenHash(ctx, id, tokenHash)
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if got != p.ID {
		t.Fatalf("player = %q, want %q", got, p.ID)
	}
	if _, err := store.PlayerByTokenHash(ctx, "m_other_match", tokenHash); err != ErrNoPlayer {
		t.Fatalf("token must not resolve in another match, got %v", err)
	}
	if _, err := store.PlayerByTokenHash(ctx, id, "wrong-hash"); err != ErrNoPlayer {
		t.Fatalf("wrong token must not resolve, got %v", err)
	}
}

// TestMarkInterruptedFlags proves leftover matches are made explicit rather
// than silently resumed.
func TestMarkInterruptedFlags(t *testing.T) {
	store, _, state, id := matchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	next := state.Clone()
	next.Phase = game.PhasePlaying
	next.Turn = game.TurnState{CurrentSeat: 0, Phase: game.TurnAwaitRoll, Round: 1}
	if err := store.CommitTransition(ctx, id, next, nil); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := store.MarkInterrupted(ctx); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	row, err := store.LoadMatch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.State.Phase != game.PhaseInterrupted || !row.Interrupted {
		t.Fatalf("match should be marked interrupted: %+v", row.State)
	}
}
