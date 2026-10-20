package config

import (
	"testing"
	"time"
)

// This file covers the configuration the operator actually sets.
//
// The package had no tests at all, which is a gap the v0.2.x review recorded:
// it is the one place where a typo in an environment variable turns into a
// server that behaves differently from the one the operator intended. These
// tests cover the knobs added for the lobby's capacity behaviour, and the
// defaults that go with them.

func TestLoadDefaultsTheCapacityKnobs(t *testing.T) {
	// The defaults must be safe on their own: a server started with no
	// environment at all still bounds its registry and still rate-limits the
	// lobby, because "unset" is not a reason to be unbounded.
	t.Setenv("HIGHJACK_MATCH_IDLE_TTL", "")
	t.Setenv("HIGHJACK_LOBBY_RATE_LIMIT", "")
	t.Setenv("HIGHJACK_LOBBY_RATE_WINDOW", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MatchIdleTTL != time.Hour {
		t.Fatalf("MatchIdleTTL = %v, want 1h", cfg.MatchIdleTTL)
	}
	if cfg.LobbyRateLimit != 60 {
		t.Fatalf("LobbyRateLimit = %d, want 60", cfg.LobbyRateLimit)
	}
	if cfg.LobbyRateWindow != time.Minute {
		t.Fatalf("LobbyRateWindow = %v, want 1m", cfg.LobbyRateWindow)
	}
}

func TestLoadReadsTheCapacityKnobs(t *testing.T) {
	t.Setenv("HIGHJACK_MATCH_IDLE_TTL", "15m")
	t.Setenv("HIGHJACK_LOBBY_RATE_LIMIT", "5")
	t.Setenv("HIGHJACK_LOBBY_RATE_WINDOW", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MatchIdleTTL != 15*time.Minute {
		t.Fatalf("MatchIdleTTL = %v, want 15m", cfg.MatchIdleTTL)
	}
	if cfg.LobbyRateLimit != 5 {
		t.Fatalf("LobbyRateLimit = %d, want 5", cfg.LobbyRateLimit)
	}
	if cfg.LobbyRateWindow != 30*time.Second {
		t.Fatalf("LobbyRateWindow = %v, want 30s", cfg.LobbyRateWindow)
	}
}

func TestLoadRejectsMalformedCapacityKnobs(t *testing.T) {
	t.Setenv("HIGHJACK_MATCH_IDLE_TTL", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of a malformed idle TTL")
	}

	t.Setenv("HIGHJACK_MATCH_IDLE_TTL", "")
	t.Setenv("HIGHJACK_LOBBY_RATE_WINDOW", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of a malformed rate window")
	}

	t.Setenv("HIGHJACK_LOBBY_RATE_WINDOW", "")
	t.Setenv("HIGHJACK_LOBBY_RATE_LIMIT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of a malformed rate limit")
	}
}

func TestValidateRejectsOutOfRangeCapacityKnobs(t *testing.T) {
	base := func() *Config {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return cfg
	}

	cfg := base()
	cfg.MatchIdleTTL = -time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected rejection of a negative idle TTL")
	}

	cfg = base()
	cfg.LobbyRateLimit = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected rejection of a negative rate limit")
	}

	cfg = base()
	cfg.LobbyRateWindow = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected rejection of a zero rate window")
	}
}

func TestALobbyRateLimitOfZeroDisablesTheLimit(t *testing.T) {
	t.Setenv("HIGHJACK_LOBBY_RATE_LIMIT", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LobbyRateLimit != 0 {
		t.Fatalf("LobbyRateLimit = %d, want 0", cfg.LobbyRateLimit)
	}
	// Zero is a valid configuration: it turns the limit off.
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a disabled rate limit must be valid: %v", err)
	}
}
