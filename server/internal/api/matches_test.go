package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/match"
)

// This file covers the HTTP surface's refusals.
//
// A refusal is a contract. `POST /matches` answering 500 when the registry is
// full told an operator reading the logs that the process was broken, when in
// fact it was working correctly and declining more work. The status an endpoint
// returns for "I cannot take this right now" is what an orchestrator and a load
// balancer act on, so getting it wrong is a correctness problem rather than a
// cosmetic one.

func matchServer(t *testing.T, registry *match.Registry) *Server {
	t.Helper()
	cfg := &config.Config{
		Env:               config.EnvTest,
		Addr:              "127.0.0.1:0",
		HeartbeatMs:       1000,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   time.Second,
	}
	srv := New(cfg, slog.New(slog.DiscardHandler), nil)
	srv.MountMatches(registry)
	return srv
}

func postMatch(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/matches", strings.NewReader("{}"))
	srv.handler().ServeHTTP(rec, req)
	return rec
}

// TestCreateMatchRefusesWithServiceUnavailableWhenTheRegistryIsFull fills the
// registry to its bound and asserts the refusal is reported as a capacity
// condition rather than as a server fault.
func TestCreateMatchRefusesWithServiceUnavailableWhenTheRegistryIsFull(t *testing.T) {
	registry := match.NewRegistry(slog.New(slog.DiscardHandler), nil)
	srv := matchServer(t, registry)

	// Fill the registry to its bound.
	for i := 0; i < match.MaxLiveMatches; i++ {
		if _, err := registry.Create(t.Context(), game.DefaultConfig()); err != nil {
			t.Fatalf("create match %d: %v", i, err)
		}
	}

	rec := postMatch(t, srv)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: a full registry is a capacity condition, not a server fault",
			rec.Code, http.StatusServiceUnavailable)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Status != "registry_full" {
		t.Fatalf("status field = %q, want registry_full", body.Status)
	}
}

// TestCreateMatchSucceedsBelowTheRegistryBound guards the other direction: the
// bound must not refuse work it has room for.
func TestCreateMatchSucceedsBelowTheRegistryBound(t *testing.T) {
	registry := match.NewRegistry(slog.New(slog.DiscardHandler), nil)
	srv := matchServer(t, registry)

	rec := postMatch(t, srv)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestLobbyIsRateLimited proves the unauthenticated lobby cannot be used to
// create matches without bound.
//
// The WebSocket transport rate-limits actions; the HTTP lobby did not. It is
// the only surface that creates a board and a durable row per match, on a path
// that requires no credential, so a single caller could fill the match
// registry.
func TestLobbyIsRateLimited(t *testing.T) {
	registry := match.NewRegistry(slog.New(slog.DiscardHandler), nil)
	cfg := &config.Config{
		Env:               config.EnvTest,
		Addr:              "127.0.0.1:0",
		HeartbeatMs:       1000,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   time.Second,
		LobbyRateLimit:    3,
		LobbyRateWindow:   time.Minute,
	}
	srv := New(cfg, slog.New(slog.DiscardHandler), nil)
	srv.MountMatches(registry)

	for i := 0; i < 3; i++ {
		rec := postMatch(t, srv)
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusCreated)
		}
	}
	rec := postMatch(t, srv)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d: the lobby must refuse a caller that has exhausted its budget",
			rec.Code, http.StatusTooManyRequests)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Status != "rate_limited" {
		t.Fatalf("status field = %q, want rate_limited", body.Status)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("a rate-limited response must tell the client when to retry")
	}
}

// TestLobbyRateLimitIsPerClientAddress proves one caller cannot exhaust
// another's budget.
func TestLobbyRateLimitIsPerClientAddress(t *testing.T) {
	registry := match.NewRegistry(slog.New(slog.DiscardHandler), nil)
	cfg := &config.Config{
		Env:               config.EnvTest,
		Addr:              "127.0.0.1:0",
		HeartbeatMs:       1000,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   time.Second,
		LobbyRateLimit:    2,
		LobbyRateWindow:   time.Minute,
	}
	srv := New(cfg, slog.New(slog.DiscardHandler), nil)
	srv.MountMatches(registry)

	post := func(addr string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/matches", strings.NewReader("{}"))
		req.RemoteAddr = addr
		srv.handler().ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 2; i++ {
		if code := post("10.0.0.1:1000"); code != http.StatusCreated {
			t.Fatalf("first client request %d: status = %d", i, code)
		}
	}
	if code := post("10.0.0.1:1000"); code != http.StatusTooManyRequests {
		t.Fatalf("first client: status = %d, want %d", code, http.StatusTooManyRequests)
	}
	if code := post("10.0.0.2:2000"); code != http.StatusCreated {
		t.Fatalf("second client: status = %d, want %d: one client must not exhaust another's budget",
			code, http.StatusCreated)
	}
}

// TestCreateMatchAppliesACustomConfigSentWithoutAContentLength covers a
// streaming client.
//
// The body is read whenever the request carries one. A chunked request declares
// no length, so gating the read on `ContentLength > 0` silently ignored the
// config of every streaming client and created the *default* board while the
// caller believed its own ruleset had been accepted. The response carries the
// config hash, so the mismatch was detectable — but only by a caller that
// thought to check, and the request was accepted with 201 either way.
func TestCreateMatchAppliesACustomConfigSentWithoutAContentLength(t *testing.T) {
	registry := match.NewRegistry(slog.New(slog.DiscardHandler), nil)
	srv := matchServer(t, registry)

	custom := game.DefaultConfig()
	custom.StartingMoney = 900
	doc, err := json.Marshal(map[string]any{"config": custom})
	if err != nil {
		t.Fatalf("marshal custom config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/matches", strings.NewReader(string(doc)))
	// httptest sets a ContentLength for a strings.Reader; clear it to model a
	// chunked request, which is what a streaming client sends.
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created createMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	customHash, err := custom.Hash()
	if err != nil {
		t.Fatalf("hash custom config: %v", err)
	}
	if created.ConfigHash != customHash {
		defaultCfg := game.DefaultConfig()
		defaultHash, err := defaultCfg.Hash()
		if err != nil {
			t.Fatalf("hash default config: %v", err)
		}
		t.Fatalf("config hash = %q, want the submitted config %q (the default is %q): "+
			"the custom config was accepted but not applied",
			created.ConfigHash, customHash, defaultHash)
	}
}
