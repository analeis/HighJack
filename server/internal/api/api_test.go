package api

import (
	"context"
	"encoding/json"
	"errors"
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
