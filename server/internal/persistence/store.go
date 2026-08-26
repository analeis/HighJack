// Package persistence owns PostgreSQL connectivity and the SQL migration
// runner.
//
// Boundaries:
//   - The rest of the server depends on repository interfaces defined
//     where they are consumed (not yet needed in v0.1).
//   - Schema truth lives in server/migrations/*.sql, applied explicitly.
//     There is no ORM auto-schema generation anywhere in this codebase.
package persistence

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps the connection pool. All fields are immutable after Connect.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// Connect opens a pool and verifies connectivity with a ping.
func Connect(ctx context.Context, log *slog.Logger, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 8 // modest default; matches v0.1 scale
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	store := &Store{pool: pool, log: log}
	if err := store.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return store, nil
}

// Ping verifies the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("persistence: store is not configured")
	}
	return s.pool.Ping(ctx)
}

// Pool exposes the raw pool to repository implementations that live in
// this package's consumer boundary. Nothing outside persistence should
// import pgx directly.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases all pool resources.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.pool.Close()
	return nil
}
