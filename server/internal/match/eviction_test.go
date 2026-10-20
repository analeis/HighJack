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
