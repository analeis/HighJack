package match

import (
	"context"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/game"
)

// This file covers the registry's capacity behaviour.
//
// The registry previously only ever grew. `POST /matches` is unauthenticated and
// unrated, so a caller could fill it to its bound and keep it full: every later
// request was refused, and because nothing was ever removed, the refusal was
// permanent for the life of the process. The bound was therefore not a safety
// limit but a denial of service that any script could trigger.

func TestEvictIdleRemovesMatchesNobodyIsPlaying(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches immediately, want 0", got)
	}
	if reg.Get(m.ID()) == nil {
		t.Fatal("a fresh match must not be evicted")
	}

	// Age the match past the TTL.
	m.mu.Lock()
	m.lastActivity = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	if got := reg.EvictIdle(time.Hour); got != 1 {
		t.Fatalf("evicted %d matches, want 1", got)
	}
	if reg.Get(m.ID()) != nil {
		t.Fatal("an idle match must be evicted")
	}
}

func TestEvictIdleKeepsMatchesWithAConnectedClient(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A client that has bound a session is playing, however long it sits.
	_, token, err := m.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := m.Bind("s1", token, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	m.mu.Lock()
	m.lastActivity = time.Now().Add(-100 * time.Hour)
	m.mu.Unlock()

	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches with a connected client, want 0", got)
	}
	if reg.Get(m.ID()) == nil {
		t.Fatal("a match with a connected client must survive eviction")
	}
}

func TestEvictIdleLeavesRecentMatchesAlone(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m.mu.Lock()
	m.lastActivity = time.Now().Add(-30 * time.Minute)
	m.mu.Unlock()

	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches inside the TTL, want 0", got)
	}
	if reg.Get(m.ID()) == nil {
		t.Fatal("a match inside the TTL must survive")
	}
}

func TestEvictIdleIsDisabledByANonPositiveTTL(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	if _, err := reg.Create(context.Background(), game.DefaultConfig()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := reg.EvictIdle(0); got != 0 {
		t.Fatalf("evicted %d matches with a zero TTL, want 0", got)
	}
	if got := reg.EvictIdle(-time.Hour); got != 0 {
		t.Fatalf("evicted %d matches with a negative TTL, want 0", got)
	}
}

// TestActivityRefreshesTheIdleClock proves the TTL measures inactivity rather
// than age. A match that keeps being played must not be reclaimed.
func TestActivityRefreshesTheIdleClock(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, token, err := m.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := m.Bind("s1", token, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	m.mu.Lock()
	m.lastActivity = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	// An action is a change, and a change is activity.
	if _, err := m.ApplyAction(context.Background(), "s1", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready: %v", err)
	}

	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches after activity, want 0", got)
	}
}

// TestTheIdleClockStartsWhenTheLastClientLeaves is the case the old clock got
// wrong.
//
// The clock used to be stamped on every action. A player who connected, sat
// still for an hour, and then disconnected had a clock that still read from
// their last move, so the sweep evicted the table on the very next pass — the
// one moment they were not there was enough to lose it.
//
// The clock now starts when the last connected client leaves, so a table that
// has just become unattended still gets its full window to be reconnected to.
func TestTheIdleClockStartsWhenTheLastClientLeaves(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, token, err := m.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := m.Bind("s1", token, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// The client's last move was two hours ago. It has been connected ever
	// since, and is leaving now.
	m.mu.Lock()
	m.lastActivity = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	m.Disconnect("s1")

	m.mu.Lock()
	clock := m.lastActivity
	m.mu.Unlock()
	if time.Since(clock) > time.Minute {
		t.Fatalf("the idle clock was not reset when the last client left: it reads %v old", time.Since(clock))
	}

	// And the table is not reclaimed straight away.
	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches immediately after the last client left, want 0", got)
	}
}

// TestAJoinIsActivity proves a claimed seat keeps the table alive even though
// nobody has connected to it yet.
func TestAJoinIsActivity(t *testing.T) {
	reg := NewRegistry(testLogger(), nil)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	m.mu.Lock()
	m.lastActivity = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	if _, _, err := m.Join(context.Background(), "Ace"); err != nil {
		t.Fatalf("join: %v", err)
	}
	if got := reg.EvictIdle(time.Hour); got != 0 {
		t.Fatalf("evicted %d matches after a join, want 0", got)
	}
}
