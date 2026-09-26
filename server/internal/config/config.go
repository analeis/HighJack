// Package config loads and validates server configuration from the
// environment. Configuration is read once at startup; there is no global
// mutable state and no hidden reads — main wires the resulting struct
// explicitly into every component that needs it.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Env names the deployment environment.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
	EnvTest        Env = "test"
)

// Config is the fully-resolved server configuration.
type Config struct {
	Env         Env
	Addr        string
	DatabaseURL string // empty = persistence disabled (valid for v0.1 dev)
	LogLevel    string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	// HeartbeatMs is advertised to realtime clients in `welcome`.
	HeartbeatMs int

	// AllowedOrigins are the browser origins permitted to call the HTTP
	// lobby and open a WebSocket. Empty means "same origin only", which is
	// the right default when the API is served behind one host.
	AllowedOrigins []string
}

// Load resolves configuration from environment variables with sane,
// documented defaults.
//
//	HIGHJACK_ENV            development | production | test   (default development)
//	HIGHJACK_ADDR           listen address                    (default :8080)
//	HIGHJACK_DATABASE_URL   postgres DSN                      (default "" = disabled)
//	HIGHJACK_LOG_LEVEL      debug|info|warn|error             (default info)
func Load() (*Config, error) {
	cfg := &Config{
		Env:               Env(getenv("HIGHJACK_ENV", string(EnvDevelopment))),
		Addr:              getenv("HIGHJACK_ADDR", ":8080"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("HIGHJACK_DATABASE_URL")),
		LogLevel:          getenv("HIGHJACK_LOG_LEVEL", "info"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		HeartbeatMs:       30_000,
	}
	if raw := strings.TrimSpace(os.Getenv("HIGHJACK_ALLOWED_ORIGINS")); raw != "" {
		for _, origin := range strings.Split(raw, ",") {
			if o := strings.TrimSpace(origin); o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks semantic correctness of the resolved configuration.
func (c *Config) Validate() error {
	switch c.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		return fmt.Errorf("config: HIGHJACK_ENV %q is not one of development|production|test", c.Env)
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("config: HIGHJACK_ADDR %q is not a valid host:port", c.Addr)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: shutdown timeout must be positive")
	}
	if c.HeartbeatMs < 0 {
		return fmt.Errorf("config: heartbeat must not be negative")
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("config: HIGHJACK_ALLOWED_ORIGINS entry %q is not an origin", origin)
		}
		if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("config: HIGHJACK_ALLOWED_ORIGINS entry %q must be scheme://host only", origin)
		}
	}
	return nil
}

// DevMode reports whether non-production behavior (verbose errors, relaxed
// CORS for local frontends) is allowed.
func (c *Config) DevMode() bool { return c.Env != EnvProduction }

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
