// This file holds the registry: the map of live matches, and the capacity
// rules around it.
//
// The registry is the runtime's front door — a match is created here, looked up
// here, and reclaimed here. It is split out of `match.go` because the capacity
// policy (the bound, the eviction, and the reason a nil store is normalised) is
// a concern of its own, and because a file that holds both the registry and the
// per-match runtime is a file nobody can find anything in.

package match

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/analeis/highjack/server/internal/game"
)

// Registry holds live matches keyed by id.
type Registry struct {
	mu      sync.RWMutex
	matches map[game.GameID]*Match
	store   Store
	log     *slog.Logger
}

// NewRegistry builds an empty registry. store may be nil: matches then run
// in memory only (development default, as in v0.1) and docs record that
// limitation.
// NewRegistry builds a registry.
//
// The store is normalised here so that "no persistence" is a genuinely nil
// interface. Passing a typed nil `*persistence.Store` leaves the interface
// non-nil, and every `store != nil` guard in this package then passes, so the
// runtime calls a method on a nil receiver. That is not hypothetical: it made
// `POST /matches` fail with 500 whenever the server ran without a database, which
// is the normal local development mode. Normalising in one place means no caller
// — including the tests — can reintroduce it.
func NewRegistry(log *slog.Logger, store Store) *Registry {
	r := &Registry{matches: make(map[game.GameID]*Match), log: log}
	if !isNilStore(store) {
		r.store = store
	}
	return r
}

// isNilStore reports whether a Store is absent, including the typed-nil case that
// a plain `== nil` comparison misses.
func isNilStore(s Store) bool {
	if s == nil {
		return true
	}
	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

// MarkInterruptedFlags runs at boot: any match left active by a previous
// process is marked interrupted rather than silently resumed. v0.2 does
// not restore live matches across restarts.
func (r *Registry) MarkInterruptedFlags(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	n, err := r.store.MarkInterrupted(ctx)
	if err != nil {
		return fmt.Errorf("mark interrupted: %w", err)
	}
	if n > 0 {
		r.log.Warn("marked leftover matches interrupted", "count", n)
	}
	return nil
}

// MaxLiveMatches bounds the registry. POST /matches is unauthenticated and
// unrated, and every match retains a full board, a bounded history ring and a
// session table, so an unbounded registry is a trivial way to exhaust memory.
//
// Exported because the API reports the bound when it refuses a request: an
// operator reading the logs should be able to see the limit they are being
// held to.
const MaxLiveMatches = 512

// ErrRegistryFull is returned once the live-match bound is reached.
var ErrRegistryFull = errors.New("match: too many live matches")

// Create mints a private match with an unguessable id and root seed.
func (r *Registry) Create(ctx context.Context, cfg game.GameConfig) (*Match, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	r.mu.RLock()
	full := len(r.matches) >= MaxLiveMatches
	r.mu.RUnlock()
	if full {
		return nil, ErrRegistryFull
	}
	id, err := newMatchID()
	if err != nil {
		return nil, err
	}
	matchID := game.GameID(id)
	seed, err := game.NewSeed()
	if err != nil {
		return nil, fmt.Errorf("generate seed: %w", err)
	}
	engine, err := game.NewEngine(&cfg, seed, game.MatchRuleset{})
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}
	state := engine.NewState(matchID)
	// The board exists from creation so a lobby snapshot already carries
	// the full, authoritative space list.
	state.Board = game.NewBoardState(&cfg)
	hash, err := cfg.Hash()
	if err != nil {
		return nil, fmt.Errorf("hash config: %w", err)
	}
	m := &Match{
		id: matchID, cfg: &cfg, hash: hash, seedHex: seed.Hex(), engine: engine,
		state: state, sessions: make(map[string]*session),
		players: make(map[game.PlayerID]string), store: r.store, log: r.log,
		lastActivity: time.Now(),
	}
	if m.store != nil {
		if err := m.store.CreateMatch(ctx, matchID, &cfg, state, seed.Hex()); err != nil {
			return nil, fmt.Errorf("persist match: %w", err)
		}
	}
	r.mu.Lock()
	r.matches[matchID] = m
	r.mu.Unlock()
	r.log.Info("match created", "match", id, "configHash", hash)
	return m, nil
}

// Get returns a live match or nil.
func (r *Registry) Get(id game.GameID) *Match {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.matches[id]
}

// EvictIdle removes matches that nobody is playing and that have not changed
// for longer than ttl, and returns how many were removed.
//
// The registry previously only ever grew. `POST /matches` is unauthenticated
// and unrated, so a caller could fill the registry to its bound and keep it
// full: every later request was refused, and because nothing was ever removed,
// the refusal was permanent for the life of the process. The bound turned a
// rate limit into a denial of service.
//
// A match with a connected client is never evicted, however long it sits. That
// is the rule that keeps this safe: a slow game, a player thinking about their
// next move, and a lobby waiting for a second player are all left alone. Only
// matches nobody is connected to are reclaimed.
//
// Evicting a match does not remove its durable rows. The database keeps the
// record; the registry simply stops serving the match, which is what "live
// matches are not restored across restarts" already implies.
func (r *Registry) EvictIdle(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-ttl)

	r.mu.Lock()
	defer r.mu.Unlock()
	evicted := 0
	for id, m := range r.matches {
		if m.evictableAt(cutoff) {
			delete(r.matches, id)
			evicted++
		}
	}
	if evicted > 0 {
		r.log.Info("evicted idle matches", "count", evicted, "ttl", ttl.String())
	}
	return evicted
}

// evictableAt reports whether the match has no connected client and has not
// changed since the cutoff. Callers hold r.mu; the match's own lock is taken
// here, so the registry lock is held while a match lock is acquired. The
// ordering is the reverse of every other path, which takes a match lock and
// then the registry lock — but the registry lock is only ever taken for a
// lookup, and never while a match lock is held, so the two cannot deadlock.
func (m *Match) evictableAt(cutoff time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) > 0 {
		return false
	}
	return m.lastActivity.Before(cutoff)
}

// ID returns the match identifier.
