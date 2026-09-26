package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/logging"
)

func testServer(t *testing.T, readiness func(context.Context) error) *Server {
	t.Helper()
	cfg := &config.Config{
		Env:               config.EnvTest,
		Addr:              "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   2 * time.Second,
	}
	log := logging.New(logging.Options{Service: "highjack-test", Level: logging.LevelError})
	return New(cfg, log, readiness)
}

func TestHealthEndpoint(t *testing.T) {
	s := testServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q", body.Status)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestReadyEndpointReflectsDependencies(t *testing.T) {
	s := testServer(t, func(context.Context) error { return nil })
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ready") {
		t.Fatalf("ready with healthy deps: %d %s", rec.Code, rec.Body.String())
	}

	failing := testServer(t, func(context.Context) error { return errors.New("db unreachable") })
	bad := httptest.NewRecorder()
	failing.handler().ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if bad.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready with failing deps must be 503, got %d", bad.Code)
	}
	if strings.Contains(bad.Body.String(), "db unreachable") {
		t.Fatal("internal error detail must not leak to clients")
	}
}

func TestVersionEndpoint(t *testing.T) {
	s := testServer(t, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "version", "protocolVersion", "schemaVersion"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("version payload missing %q: %v", key, body)
		}
	}
}

func TestUnknownRouteIs404JSONish(t *testing.T) {
	s := testServer(t, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	s := testServer(t, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing security header %q", h)
		}
	}
}

func TestRequestIDGeneratedAndEchoed(t *testing.T) {
	s := testServer(t, nil)

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	first := rec.Header().Get("X-Request-Id")
	if first == "" {
		t.Fatal("server must mint a request id when none supplied")
	}

	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-Id", "my-trace-id-42")
	s.handler().ServeHTTP(rec2, req)
	if got := rec2.Header().Get("X-Request-Id"); got != "my-trace-id-42" {
		t.Fatalf("caller-supplied request id not honored: %q", got)
	}

	// Header injection via newline must be rejected/regenerated.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req3.Header.Set("X-Request-Id", "bad\nid")
	s.handler().ServeHTTP(rec3, req3)
	if got := rec3.Header().Get("X-Request-Id"); got == "bad\nid" || got == "" {
		t.Fatalf("malicious request id mishandled: %q", got)
	}
}

func TestPanicRecoveredAs500(t *testing.T) {
	s := testServer(t, nil)
	mux := s.router
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic must become 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("panic value must not leak into the response")
	}
}

func TestGracefulShutdownCompletes(t *testing.T) {
	s := testServer(t, nil)

	listenErr := make(chan error, 1)
	go func() { listenErr <- s.ListenAndServe() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("shutdown took too long: %v", elapsed)
	}
	select {
	case err := <-listenErr:
		if err != nil {
			t.Fatalf("listener error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

// TestCORSEchoesOnlyAllowedOrigins proves the CORS allow-list is exact:
// a configured origin is echoed, an unconfigured one is not, and preflight
// short-circuits without reaching the handler.
func TestCORSEchoesOnlyAllowedOrigins(t *testing.T) {
	cfg := &config.Config{
		Addr:              ":0",
		HeartbeatMs:       1000,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   time.Second,
		AllowedOrigins:    []string{"http://127.0.0.1:5173"},
	}
	srv := New(cfg, slog.New(slog.DiscardHandler), nil)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Fatalf("configured origin not echoed: %q", got)
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatal("Vary: Origin must be set for cache safety")
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured origin must not be allowed, got %q", got)
	}

	// Preflight is answered by the middleware, not the handler.
	req = httptest.NewRequest(http.MethodOptions, "/matches", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
}

func TestConfigRejectsMalformedAllowedOrigins(t *testing.T) {
	t.Setenv("HIGHJACK_ALLOWED_ORIGINS", "not-an-origin")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected rejection of malformed origin")
	}
	t.Setenv("HIGHJACK_ALLOWED_ORIGINS", "http://ok.example, http://also-ok.example/path")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected rejection of origin carrying a path")
	}
	t.Setenv("HIGHJACK_ALLOWED_ORIGINS", "http://a.example, http://b.example")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("valid origins rejected: %v", err)
	}
	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("expected 2 origins, got %d", len(cfg.AllowedOrigins))
	}
}
