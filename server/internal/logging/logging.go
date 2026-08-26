// Package logging configures HighJack's structured logging.
//
// All logs are single-line JSON on stdout with stable field names:
//
//	{"time":…,"level":"INFO","msg":"http request","service":"highjack-server","version":"0.1.0","requestId":"…"}
//
// The request-scoped logger (with requestId) is attached to request
// contexts by the API middleware. Future metrics/tracing should hook in
// here first; see docs/architecture/BACKEND.md for the observability plan.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type Level slog.Level

const (
	LevelDebug Level = Level(slog.LevelDebug)
	LevelInfo  Level = Level(slog.LevelInfo)
	LevelWarn  Level = Level(slog.LevelWarn)
	LevelError Level = Level(slog.LevelError)
)

// ParseLevel maps a config string to a level, defaulting to info.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type Options struct {
	Service string
	Version string
	Level   Level
}

// New builds the process-wide base logger. Fields set here appear on every
// log line: service name and build version at minimum.
func New(opts Options) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.Level(opts.Level),
	})
	return slog.New(handler).With(
		slog.String("service", opts.Service),
		slog.String("version", opts.Version),
	)
}

type contextKey struct{}

// WithLogger stores a (usually request-scoped) logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext returns the request-scoped logger, or a discarded no-op
// logger when none is attached.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}
