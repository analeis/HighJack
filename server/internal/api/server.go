// Package api implements the HTTP surface of the HighJack server:
// health/readiness/version endpoints plus the middleware chain
// (request IDs, structured access logs, panic recovery, security headers)
// and the graceful server lifecycle.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/logging"
	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/realtime"
)

const (
	requestIDHeader = "X-Request-Id"
	maxRequestIDLen = 128
)

// Server assembles the HTTP handler and owns the listener lifecycle.
type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	router   *http.ServeMux
	http     *http.Server
	registry *match.Registry

	// readiness reports component health; nil means always ready.
	readiness func(ctx context.Context) error
}

// New builds the API server. readiness may be nil. base is the process
// logger; request logs derive from it with a requestId field attached.
func New(cfg *config.Config, base *slog.Logger, readiness func(context.Context) error) *Server {
	s := &Server{
		cfg:       cfg,
		log:       base,
		readiness: readiness,
	}
	s.router = http.NewServeMux()
	s.routes()
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
	return s
}

func (s *Server) routes() {
	s.router.HandleFunc("GET /health", s.handleHealth)
	s.router.HandleFunc("GET /ready", s.handleReady)
	s.router.HandleFunc("GET /version", s.handleVersion)
}

// MountRealtime attaches the WebSocket transport seam at /ws so it shares
// the middleware chain (request ids, recovery, logging).
func (s *Server) MountRealtime(h *realtime.Handler) {
	h.Register(s.router)
}

// Handler exposes the fully wrapped HTTP handler (middleware chain
// included). Tests mount it on their own servers; the binary uses
// ListenAndServe.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Addr returns the configured listen address (for tests).
func (s *Server) Addr() string { return s.cfg.Addr }

// ListenAndServe starts the server and blocks until Shutdown or error.
func (s *Server) ListenAndServe() error {
	err := s.http.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully drains connections within the configured timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// ShutdownTimeout exposes the configured drain window.
func (s *Server) ShutdownTimeout() time.Duration { return s.cfg.ShutdownTimeout }

// handler wraps the router with the full middleware chain, in order:
// recover → request id → security headers → access log.
func (s *Server) handler() http.Handler {
	var h http.Handler = s.router
	h = s.accessLog(h)
	h = s.securityHeaders(h)
	h = s.withRequestID(h)
	h = s.recoverPanics(h)
	return h
}

// withRequestID assigns a random request id when the caller did not supply
// a sane one, stores it in the context for logging, and echoes it back.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if len(id) == 0 || len(id) > maxRequestIDLen || !isPrintableASCII(id) {
			var buf [8]byte
			if _, err := rand.Read(buf[:]); err != nil {
				id = fmt.Sprintf("%d", time.Now().UnixNano())
			} else {
				id = hex.EncodeToString(buf[:])
			}
		}
		w.Header().Set(requestIDHeader, id)
		reqLog := s.log.With(slog.String("requestId", id))
		next.ServeHTTP(w, r.WithContext(logging.WithLogger(r.Context(), reqLog)))
	})
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

// securityHeaders applies conservative defaults to every response.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// recoverPanics converts handler panics into 500s without leaking stack
// details to clients; the stack goes to logs instead.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logging.FromContext(r.Context()).Error("panic recovered",
					"panic", fmt.Sprint(rec), "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError,
					response{Status: "internal_error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// accessLog emits one structured line per request.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logging.FromContext(r.Context()).Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// Unwrap exposes the underlying writer so http.ResponseController (used by
// the WebSocket upgrader to reach the hijacker and deadlines) can traverse
// the middleware chain. Without it, a wrapped writer looks like it cannot
// be upgraded and the handshake fails with 501.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// ---- responses -------------------------------------------------------------

// response is the stable JSON envelope of all HTTP endpoints.
type response struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, v any) {
	writeJSON(w, code, v)
}
