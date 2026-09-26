// Command highjack is the HighJack game server binary.
//
// It wires configuration, logging, the HTTP API (health/ready/version),
// and the realtime WebSocket seam, and manages graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/analeis/highjack/server/internal/api"
	"github.com/analeis/highjack/server/internal/config"
	"github.com/analeis/highjack/server/internal/logging"
	"github.com/analeis/highjack/server/internal/match"
	"github.com/analeis/highjack/server/internal/persistence"
	"github.com/analeis/highjack/server/internal/realtime"
	"github.com/analeis/highjack/server/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "highjack-server: fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.New(logging.Options{
		Service: "highjack-server",
		Version: version.Version,
		Level:   logging.ParseLevel(cfg.LogLevel),
	})
	slog.SetDefault(log)

	log.Info("starting highjack server",
		"env", cfg.Env,
		"addr", cfg.Addr,
		"commit", version.Commit,
	)

	// Persistence is optional in v0.1: the server runs without a database
	// for local frontend development. When configured, connectivity is
	// established up front and reflected in /ready.
	var store *persistence.Store
	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		store, err = persistence.Connect(ctx, log, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		defer func() {
			if err := store.Close(); err != nil {
				log.Warn("database close failed", "error", err.Error())
			}
		}()
		log.Info("database connected")

		// Apply migrations at boot. Without this, a server started against an
		// empty database reported "database connected" and answered /ready with
		// 200, then failed every write with a missing-relation error. Refusing to
		// start is the honest outcome: the operator asked for a database, and a
		// database without a schema is not one.
		dir := os.Getenv("HIGHJACK_MIGRATIONS_DIR")
		if dir == "" {
			dir = "server/migrations"
		}
		migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
		applied, err := store.Migrate(migrateCtx, dir)
		cancelMigrate()
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if len(applied) > 0 {
			log.Info("applied migrations", "count", len(applied))
		} else {
			log.Info("schema already current")
		}
	} else {
		log.Info("no database configured; persistence disabled (HIGHJACK_DATABASE_URL unset)")
	}

	readiness := func(ctx context.Context) error {
		if store == nil {
			return nil // no dependencies to check yet
		}
		// A ping proves the socket is open, not that the schema is there. Check the
		// schema too, so a database that lost its tables takes the instance out of
		// rotation instead of failing every request.
		if err := store.Ping(ctx); err != nil {
			return err
		}
		return store.CheckSchema(ctx)
	}

	// Live matches are not restored across process restarts in v0.2: any
	// match left active by a previous process is marked interrupted at
	// boot instead of being silently resumed from partial data.
	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelBoot()
	registry := match.NewRegistry(log, store)
	// A failure here is logged, not fatal: marking leftovers interrupted is a
	// safety measure, and refusing to start because of it would turn a cosmetic
	// problem into an outage. Readiness still reports the schema's health.
	if err := registry.MarkInterruptedFlags(bootCtx); err != nil {
		log.Error("failed to mark interrupted matches", "error", err.Error())
	}

	server := api.New(cfg, log, readiness)
	server.MountMatches(registry)
	realtimeHandler := realtime.NewHandler(log, cfg.HeartbeatMs, registry, cfg.AllowedOrigins, cfg.DevMode())
	server.MountRealtime(realtimeHandler)

	// Signal-driven lifecycle: serve until SIGINT/SIGTERM, then drain.
	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err.Error())
			return fmt.Errorf("serve: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown incomplete", "error", err.Error())
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}
