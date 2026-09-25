// Package persistence owns PostgreSQL connectivity, the SQL migration
// runner, and the authoritative match repository.
//
// Boundary: everything above this package works with domain values
// (game.GameState, game.Event) and never touches pgx. A transition and its
// events are written in one transaction, so a match can never be observed
// with state that its event log does not explain.
package persistence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/analeis/highjack/server/internal/game"
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
		return errors.New("persistence: store is not configured")
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

// ---- match repository -------------------------------------------------------

// MatchRow is the durable projection of a match plus its full state.
type MatchRow struct {
	Config      game.GameConfig
	State       game.GameState
	ConfigHash  string
	SeedHex     string
	Cursor      uint64
	Interrupted bool
}

// CreateMatch inserts the config, the match row, and the initial snapshot
// in one transaction, so a match is never visible without recoverable
// state.
func (s *Store) CreateMatch(ctx context.Context, id game.GameID, cfg *game.GameConfig, state *game.GameState, seedHex string) error {
	if s == nil {
		return errors.New("persistence: store is not configured")
	}
	doc, err := cfg.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("canonicalize config: %w", err)
	}
	hash, err := cfg.Hash()
	if err != nil {
		return fmt.Errorf("hash config: %w", err)
	}
	stateDoc, err := game.MarshalGameState(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create match: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO game_configs (id, schema_version, document, document_hash)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET document = EXCLUDED.document, document_hash = EXCLUDED.document_hash`,
		string(id)+":config", game.ConfigSchemaVersion, string(doc), hash); err != nil {
		return fmt.Errorf("insert config: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO games (id, status, config_id, seed_hex, config_hash, current_seat, current_round, turn_phase, cursor)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		string(id), string(state.Phase), string(id)+":config", seedHex, hash,
		state.Turn.CurrentSeat, state.Turn.Round, string(state.Turn.Phase), state.Tick); err != nil {
		return fmt.Errorf("insert game: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO match_snapshots (match_id, state, cursor) VALUES ($1, $2, $3)`,
		string(id), string(stateDoc), state.Tick); err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create match: %w", err)
	}
	return nil
}

// CommitTransition atomically persists the new authoritative state and the
// events that produced it, then advances the match's cursor. The caller
// must not broadcast until this returns nil: a failure means the
// transition is not durable and the in-memory state must be discarded.
func (s *Store) CommitTransition(ctx context.Context, id game.GameID, state *game.GameState, events []game.Event) error {
	if s == nil {
		return errors.New("persistence: store is not configured")
	}
	stateDoc, err := game.MarshalGameState(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin commit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i, ev := range events {
		payload, err := game.MarshalEvent(ev)
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", ev.Type(), err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO match_events (match_id, tick, seq, kind, payload)
			 VALUES ($1, $2, $3, $4, $5)`,
			string(id), ev.Tick(), i, string(ev.Type()), string(payload)); err != nil {
			return fmt.Errorf("insert event %s: %w", ev.Type(), err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE games SET status = $2, current_seat = $3, current_round = $4, turn_phase = $5,
		                  doubles_streak = $6, award_extra_roll = $7, cursor = $8
		 WHERE id = $1`,
		string(id), string(state.Phase), state.Turn.CurrentSeat, state.Turn.Round,
		string(state.Turn.Phase), state.Turn.DoublesStreak, state.Turn.AwardExtraRoll, state.Tick); err != nil {
		return fmt.Errorf("update game: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO match_snapshots (match_id, state, cursor) VALUES ($1, $2, $3)
		 ON CONFLICT (match_id) DO UPDATE SET state = EXCLUDED.state, cursor = EXCLUDED.cursor, written_at = now()`,
		string(id), string(stateDoc), state.Tick); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}
	return nil
}

// LoadMatch reconstructs a match from its snapshot. Returns ErrNoMatch when
// the match does not exist.
func (s *Store) LoadMatch(ctx context.Context, id game.GameID) (*MatchRow, error) {
	if s == nil {
		return nil, errors.New("persistence: store is not configured")
	}
	var (
		seedHex     string
		hash        string
		cursor      int64
		interrupted bool
		configDoc   []byte
		stateDoc    []byte
	)
	err := s.pool.QueryRow(ctx,
		`SELECT g.seed_hex, g.config_hash, g.cursor, g.interrupted, c.document, m.state
		 FROM games g
		 JOIN game_configs c ON c.id = g.config_id
		 JOIN match_snapshots m ON m.match_id = g.id
		 WHERE g.id = $1`, string(id)).
		Scan(&seedHex, &hash, &cursor, &interrupted, &configDoc, &stateDoc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoMatch
	}
	if err != nil {
		return nil, fmt.Errorf("load match: %w", err)
	}
	cfg, err := game.ParseGameConfig(configDoc)
	if err != nil {
		return nil, fmt.Errorf("parse stored config: %w", err)
	}
	state, err := game.UnmarshalGameState(stateDoc)
	if err != nil {
		return nil, fmt.Errorf("parse stored state: %w", err)
	}
	return &MatchRow{
		Config:      *cfg,
		State:       *state,
		ConfigHash:  hash,
		SeedHex:     seedHex,
		Cursor:      uint64(cursor),
		Interrupted: interrupted,
	}, nil
}

// SavePlayer upserts a player's membership row including the hashed
// reconnect token.
func (s *Store) SavePlayer(ctx context.Context, matchID game.GameID, player game.Player, tokenHash string) error {
	if s == nil {
		return errors.New("persistence: store is not configured")
	}
	userID := string(player.ID)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, display_name) VALUES ($1, $2)
		 ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		userID, player.Name); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO game_players (game_id, user_id, seat, money, status, position, token_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (game_id, seat) DO UPDATE
		   SET money = EXCLUDED.money, status = EXCLUDED.status,
		       position = EXCLUDED.position, token_hash = EXCLUDED.token_hash`,
		string(matchID), userID, player.Seat, int64(player.Money),
		string(player.Status), player.Position, tokenHash); err != nil {
		return fmt.Errorf("upsert player: %w", err)
	}
	return nil
}

// PlayerByTokenHash resolves a reconnect token to its player id within a
// match. Returns ErrNoPlayer when the token does not belong to that match.
func (s *Store) PlayerByTokenHash(ctx context.Context, matchID game.GameID, tokenHash string) (game.PlayerID, error) {
	if s == nil {
		return "", errors.New("persistence: store is not configured")
	}
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM game_players WHERE game_id = $1 AND token_hash = $2`,
		string(matchID), tokenHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoPlayer
	}
	if err != nil {
		return "", fmt.Errorf("lookup player by token: %w", err)
	}
	return game.PlayerID(id), nil
}

// MarkInterruptedFlags moves every non-ended match to 'interrupted'. Run at
// boot: v0.2 does not restore live matches across process restarts, and an
// interrupted match must be visible as such rather than silently resumed.
func (s *Store) MarkInterrupted(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, errors.New("persistence: store is not configured")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE games SET status = 'interrupted', interrupted = TRUE
		 WHERE status IN ('lobby', 'playing')`)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ErrNoMatch and ErrNoPlayer are the repository's absence signals.
var (
	ErrNoMatch  = errors.New("match not found")
	ErrNoPlayer = errors.New("player not found")
)
