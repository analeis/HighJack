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

func TestLoadReadsTheCoreEnvironment(t *testing.T) {
	t.Setenv("HIGHJACK_ENV", "production")
	t.Setenv("HIGHJACK_ADDR", "127.0.0.1:9999")
	t.Setenv("HIGHJACK_DATABASE_URL", "postgres://example/db")
	t.Setenv("HIGHJACK_LOG_LEVEL", "warn")
	t.Setenv("HIGHJACK_ALLOWED_ORIGINS", "http://a.example, http://b.example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Env != EnvProduction {
		t.Fatalf("Env = %q, want production", cfg.Env)
	}
	if cfg.Addr != "127.0.0.1:9999" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DatabaseURL != "postgres://example/db" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("AllowedOrigins = %v, want two entries", cfg.AllowedOrigins)
	}
	if cfg.AllowedOrigins[0] != "http://a.example" || cfg.AllowedOrigins[1] != "http://b.example" {
		t.Fatalf("AllowedOrigins = %v, want the origins trimmed", cfg.AllowedOrigins)
	}
}

func TestLoadRejectsAnUnknownEnvironment(t *testing.T) {
	t.Setenv("HIGHJACK_ENV", "staging")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of an unknown environment")
	}
}

func TestLoadRejectsAMalformedAddress(t *testing.T) {
	t.Setenv("HIGHJACK_ADDR", "not-an-address")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of a malformed address")
	}
}

func TestLoadRejectsAnOriginWithAPath(t *testing.T) {
	t.Setenv("HIGHJACK_ALLOWED_ORIGINS", "http://a.example/some/path")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection of an origin carrying a path")
	}
}

func TestAnEmptyDatabaseURLDisablesPersistence(t *testing.T) {
	t.Setenv("HIGHJACK_DATABASE_URL", "   ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty: a blank DSN means persistence is off", cfg.DatabaseURL)
	}
}

// TestDevModeIsOnlyOffInProduction is a security-relevant default.
//
// Dev mode relaxes the origin check: a browser from an origin the server
// cannot verify is accepted. Outside production that is a convenience; in
// production it would be a hole, so the environment has to be set explicitly
// for the server to be safe.
func TestDevModeIsOnlyOffInProduction(t *testing.T) {
	t.Setenv("HIGHJACK_ENV", "production")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DevMode() {
		t.Fatal("production must not be in development mode")
	}

	t.Setenv("HIGHJACK_ENV", "development")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.DevMode() {
		t.Fatal("development must be in development mode")
	}

	// The test environment is not production either.
	t.Setenv("HIGHJACK_ENV", "test")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.DevMode() {
		t.Fatal("test must be in development mode")
	}
}

func TestTheDefaultEnvironmentIsDevelopment(t *testing.T) {
	// This is the setting an operator gets by omission, so it is worth stating
	// explicitly: the server starts permissive unless HIGHJACK_ENV says
	// otherwise.
	t.Setenv("HIGHJACK_ENV", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Fatalf("Env = %q, want development", cfg.Env)
	}
}
