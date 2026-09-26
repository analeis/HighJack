// Package match owns the authoritative runtime of a live HighJack match:
// the registry of matches, per-match serialized execution, session/actor
// binding, event history for recovery, and the durability hook.
//
// The engine stays pure (no locks, no IO); this package serializes access
// around it. Every accepted transition is committed to storage before its
// events are published, so nothing is ever broadcast that recovery could
// not reproduce.
package match

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/protocol"
)

// historyCap bounds per-match event retention (v0.2 decision). A cursor
// older than the retained base gets a snapshot instead of a catch-up.
const historyCap = 1024

// Sink is a connected client the runtime can publish to. Send must be
// non-blocking with respect to the match lock: implementations must never
// block indefinitely on a slow consumer.
type Sink interface {
	Send(msg any) bool
}

// Store is the persistence surface the runtime depends on.
//
// Deliberately narrow. A fake cannot prove SQL, transactions, or constraints —
// that is what the PostgreSQL integration suite is for — but it *can* prove the
// properties that live in this package: that a failed commit leaves no
// in-memory trace, that a failed join leaves no seat behind, and that an
// indeterminate commit is reconciled rather than trusted. Those are exactly
// the invariants that were untestable while the runtime held a concrete
// *persistence.Store.
type Store interface {
	CreateMatch(ctx context.Context, id game.GameID, cfg *game.GameConfig, state *game.GameState, seedHex string) error
	SavePlayer(ctx context.Context, matchID game.GameID, player game.Player, tokenHash string) error
	CommitTransition(ctx context.Context, id game.GameID, state *game.GameState, events []game.Event) error
	MarkInterrupted(ctx context.Context) (int64, error)
}

// EventEnvelope pairs an event with the tick that produced it, matching
// the wire shape in packages/protocol (catchup entries and event frames).
type EventEnvelope struct {
	Tick  uint64
	Event game.Event
}

// DomainError maps an engine rejection onto a protocol error code.
func DomainError(err error) protocol.ErrorCode {
	switch {
	case errors.Is(err, game.ErrNotYourTurn), errors.Is(err, game.ErrNotPermitted),
		errors.Is(err, game.ErrPlayerNotInGame):
		return protocol.CodeNotPermitted
	case errors.Is(err, game.ErrOutOfPhase), errors.Is(err, game.ErrAlreadyStarted):
		return protocol.CodeOutOfPhase
	case errors.Is(err, game.ErrGameFull):
		return protocol.CodeGameFull
	case errors.Is(err, game.ErrNotEnoughPlayers):
		return protocol.CodeNotEnoughPlayers
	case errors.Is(err, game.ErrNotAllReady), errors.Is(err, game.ErrInvalidName),
		errors.Is(err, game.ErrUnknownAction):
		return protocol.CodeInvalidAction
	default:
		return protocol.CodeInternalError
	}
}

// Match is one authoritative runtime. The zero value is not usable; create
// matches through a Registry.
type Match struct {
	id      game.GameID
	cfg     *game.GameConfig
	hash    string
	seedHex string
	engine  *game.Engine

	// mu serializes every state mutation and the history/cursor update.
	// Network writes happen outside it.
	mu      sync.Mutex
	state   *game.GameState
	history []EventEnvelope
	base    uint64 // oldest retained tick (0 when history is empty)

	sessions map[string]*session
	players  map[game.PlayerID]string // player id → hashed token

	store Store
	log   *slog.Logger
}

type session struct {
	id     string
	player game.PlayerID
	sink   Sink
	bound  bool

	// lastSeq is the per-connection action watermark. Repeating the last
	// sequence replays the cached ack; anything older is stale.
	lastSeq   int64
	lastAck   ActionResult
	haveAck   bool
	connected bool
}

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
func NewRegistry(log *slog.Logger, store Store) *Registry {
	return &Registry{matches: make(map[game.GameID]*Match), store: store, log: log}
}

// MarkInterruptedFlags runs at boot: any match left active by a previous
// process is marked interrupted rather than silently resumed. v0.2 does
// not restore live matches across restarts.
func (r *Registry) MarkInterruptedFlags(ctx context.Context) {
	if r.store == nil {
		return
	}
	n, err := r.store.MarkInterrupted(ctx)
	if err != nil {
		r.log.Error("failed to mark interrupted matches", "error", err.Error())
		return
	}
	if n > 0 {
		r.log.Warn("marked leftover matches interrupted", "count", n)
	}
}

// Create mints a private match with an unguessable id and root seed.
func (r *Registry) Create(ctx context.Context, cfg game.GameConfig) (*Match, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
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

// ID returns the match identifier.
func (m *Match) ID() game.GameID { return m.id }

// Snapshot returns the authoritative state under the match lock.
func (m *Match) Snapshot() protocol.GameSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return protocol.NewGameSnapshot(m.state)
}

// Config returns the immutable validated configuration.
func (m *Match) Config() *game.GameConfig { return m.cfg }

// ---- sessions ---------------------------------------------------------------

// Join mints a seat and reconnect token for a new player. The token is
// returned once; only its sha256 is retained.
func (m *Match) Join(ctx context.Context, displayName string) (game.PlayerID, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Phase != game.PhaseLobby {
		return "", "", game.ErrAlreadyStarted
	}
	next, events, err := m.engine.Apply(m.state, "", game.PlayerJoinAction{DisplayName: displayName})
	if err != nil {
		return "", "", err
	}
	joined, ok := events[0].(*game.PlayerJoinedEvent)
	if !ok {
		return "", "", errors.New("match: join produced no player_joined event")
	}
	token, err := newID("tok_")
	if err != nil {
		return "", "", err
	}
	tokenHash := hashToken(token)

	// Persist first. A join that cannot be stored must leave nothing behind: the
	// caller is an unauthenticated HTTP endpoint whose context dies the moment
	// the client hangs up, and a seat consumed by a player whose token was then
	// discarded could never be released, because leaving requires a bindable
	// actor. Four aborted requests would fill the match for good.
	joinedPlayer := next.PlayerByID(joined.PlayerID)
	if joinedPlayer == nil {
		return "", "", errors.New("match: joined player missing from state")
	}
	if m.store != nil {
		if err := m.store.SavePlayer(ctx, m.id, *joinedPlayer, tokenHash); err != nil {
			return "", "", fmt.Errorf("persist player: %w", err)
		}
	}
	m.state = next
	m.appendHistory(events)
	m.players[joined.PlayerID] = tokenHash
	return joined.PlayerID, token, nil
}

// Bind attaches a live connection to a player via its reconnect token.
// Unknown matches, wrong tokens, and non-members are rejected; a client can
// never choose its actor by sending a player id.
func (m *Match) Bind(sessionID, token, clientID string, sink Sink) (game.PlayerID, error) {
	tokenHash := hashToken(token)
	m.mu.Lock()
	player, ok := m.playersByTokenLocked(tokenHash)
	if !ok {
		m.mu.Unlock()
		return "", ErrUnauthorized
	}
	if existing, dup := m.sessions[sessionID]; dup {
		// Rebinding the same connection id (reconnect on the same session)
		// replaces the old sink rather than leaking it.
		_ = existing
	}
	s := m.sessions[sessionID]
	if s == nil {
		s = &session{id: sessionID}
		m.sessions[sessionID] = s
	}
	s.sink = sink
	s.player = player
	s.bound = true
	s.connected = true
	// Reconnect keeps the action watermark so a replayed seq is still
	// recognized as a duplicate rather than replayed as new.
	m.mu.Unlock()
	m.log.Info("session bound", "match", m.id, "session", sessionID, "player", player)
	return player, nil
}

// playersByTokenLocked resolves a hashed token inside the match. Callers
// hold m.mu.
func (m *Match) playersByTokenLocked(tokenHash string) (game.PlayerID, bool) {
	for id, h := range m.players {
		if h == tokenHash && tokenHash != "" {
			return id, true
		}
	}
	return "", false
}

// Disconnect marks a session offline. The player keeps their seat; the
// engine's leave/elimination path is an explicit action, not a side effect
// of a dropped socket (reconnectable by design).
func (m *Match) Disconnect(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[sessionID]
	if s == nil {
		return
	}
	s.connected = false
	s.sink = nil
	// Drop the session rather than leaving a bound-but-dead entry. Session ids
	// are minted per connection and never reused, so nothing can be waiting on
	// this one, and keeping it would (a) leave a bound identity that can still
	// reach ApplyAction, and (b) retain the sink and the cached snapshot — which
	// holds every space and player — for the life of the match.
	delete(m.sessions, sessionID)
}

// ---- actions ----------------------------------------------------------------

// ActionResult is the outcome of one dispatched action.
type ActionResult struct {
	Events     []EventEnvelope
	Snapshot   protocol.GameSnapshot
	NextSeq    int64
	DomainCode protocol.ErrorCode
	Err        error
	// Replayed marks a result served from the idempotency cache rather than
	// newly applied. The transition already happened and was already
	// published, so a replayed result must never be broadcast again: doing so
	// re-delivered the same economic events to every peer, and clients that
	// folded them a second time charged rent twice.
	Replayed bool
}

// ApplyAction serializes one action from a bound session.
//
// Order of operations (v0.2 decision): validate identity and sequence,
// apply through the engine, commit state+events durably, and only then
// return for publication. A persistence failure returns an error and the
// in-memory state is rolled back to the last durable state, so a lost
// write is never broadcast.
func (m *Match) ApplyAction(ctx context.Context, sessionID string, seq int64, action game.Action) (ActionResult, error) {
	// Deferred, so no return path — including a panic from a corrupt state
	// reaching the engine — can leave the match locked forever. With manual
	// unlocks, any newly added early return or a panic silently converted one
	// bad request into a permanently wedged match.
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.sessions[sessionID]
	if s == nil || !s.bound {
		return ActionResult{DomainCode: protocol.CodeNotPermitted}, ErrUnboundSession
	}
	// Sequence numbers are 1-based and must leave room for the next-sequence
	// hint. Zero is a fresh watermark's zero value; MaxInt64 would overflow that
	// hint to a negative number and make every later sequence look stale, which
	// bricks the seat with a misleading error.
	if seq < 1 || seq == math.MaxInt64 {
		return ActionResult{DomainCode: protocol.CodeInvalidAction}, ErrInvalidSequence
	}
	// Idempotency: a repeated last seq replays the cached ack; an older
	// seq is stale. A replay carries the ack only — the events were published
	// when the transition was first applied, and re-publishing them would
	// double-apply the transition on every peer.
	if seq <= s.lastSeq {
		if !s.haveAck || seq != s.lastSeq {
			return ActionResult{DomainCode: protocol.CodeInvalidAction}, ErrStaleSequence
		}
		cached := s.lastAck
		cached.Events = nil
		cached.Replayed = true
		return cached, nil
	}

	prev := m.state
	next, events, err := m.engine.Apply(m.state, s.player, action)
	if err != nil {
		// A domain rejection is recoverable and is reported in the result, not
		// as a transport error, so the handler can distinguish it from a
		// durability failure.
		return ActionResult{DomainCode: DomainError(err), Err: err}, nil
	}

	// Durability first: state and events become durable together, and only then
	// is anything published. A failure restores the last durable state so a lost
	// write is never broadcast, and leaves the sequence watermark untouched so
	// the client may retry the same sequence.
	if m.store != nil {
		if perr := m.store.CommitTransition(ctx, m.id, next, events); perr != nil {
			m.state = prev
			m.log.Error("transition not durable; rolled back",
				"match", m.id, "error", perr.Error())
			return ActionResult{DomainCode: protocol.CodeInternalError, Err: perr}, perr
		}
	}

	envelopes := m.commitLocked(events, next)
	snap := protocol.NewGameSnapshot(next)
	s.lastSeq = seq
	s.lastAck = ActionResult{Events: envelopes, Snapshot: snap, NextSeq: seq + 1}
	s.haveAck = true
	return ActionResult{Events: envelopes, Snapshot: snap, NextSeq: seq + 1}, nil
}

// lastAckValue returns the cached acknowledgement for a duplicate seq.
// commitLocked advances the authoritative state and history under the
// lock and returns wire envelopes. The engine has already produced the
// next state (and its tick); this installs it. Callers hold m.mu.
func (m *Match) commitLocked(events []game.Event, next *game.GameState) []EventEnvelope {
	m.state = next
	envelopes := make([]EventEnvelope, 0, len(events))
	for _, ev := range events {
		envelopes = append(envelopes, EventEnvelope{Tick: ev.Tick(), Event: ev})
		m.history = append(m.history, EventEnvelope{Tick: ev.Tick(), Event: ev})
	}
	if len(m.history) > historyCap {
		drop := len(m.history) - historyCap
		m.history = append([]EventEnvelope(nil), m.history[drop:]...)
		m.base = m.history[0].Tick
	}
	return envelopes
}

// appendHistory records a transition's events and enforces the retention
// window.
//
// The window is trimmed on tick boundaries, not on raw event counts: one
// transition emits several events that all share a tick, so cutting mid-group
// would retain a *partial* transition. Since() would then serve that partial
// group as a complete catch-up, and a client would apply a purchase without its
// payment.
func (m *Match) appendHistory(events []game.Event) {
	for _, ev := range events {
		m.history = append(m.history, EventEnvelope{Tick: ev.Tick(), Event: ev})
	}
	m.trimHistoryLocked()
}

func (m *Match) trimHistoryLocked() {
	if len(m.history) <= historyCap {
		return
	}
	drop := len(m.history) - historyCap
	// Advance past every remaining event of the tick we would otherwise split.
	boundary := m.history[drop-1].Tick
	for drop < len(m.history) && m.history[drop].Tick == boundary {
		drop++
	}
	m.history = append([]EventEnvelope(nil), m.history[drop:]...)
	if len(m.history) > 0 {
		m.base = m.history[0].Tick
	}
}

// Since returns retained events strictly after a cursor, and whether the
// cursor is still catch-up-able. An empty (or too old) window means the
// client must take a fresh snapshot instead of assuming continuity.
func (m *Match) Since(cursor uint64) (events []EventEnvelope, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.history) == 0 {
		if cursor == 0 {
			return nil, true
		}
		return nil, false
	}
	if cursor+1 < m.base {
		return nil, false
	}
	// A cursor the server has never reached means the client's view is ahead of
	// authoritative state. Reporting success with no events would tell it it is
	// up to date, so it must be sent back for a snapshot instead.
	if cursor > m.state.Tick {
		return nil, false
	}
	for _, e := range m.history {
		if e.Tick > cursor {
			events = append(events, e)
		}
	}
	return events, true
}

// Broadcast publishes accepted events to every connected, bound session in
// the match. Sinks are collected under the lock and written to after it is
// released, so a slow consumer can never stall the match or a peer. Each
// event carries its tick, which is the client-side cursor and the dedup
// key (delivery is at-least-once).
func (m *Match) Broadcast(events []EventEnvelope, exceptSeq int64) {
	if len(events) == 0 {
		return
	}
	m.mu.Lock()
	sinks := make([]Sink, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.bound && s.connected && s.sink != nil {
			sinks = append(sinks, s.sink)
		}
	}
	m.mu.Unlock()

	for _, sink := range sinks {
		for _, e := range events {
			payload, err := protocol.EncodeEvent(e.Event)
			if err != nil {
				continue
			}
			var generic any
			if err := json.Unmarshal(payload, &generic); err != nil {
				continue
			}
			sink.Send(map[string]any{
				"v": protocol.ProtocolMajorVersion, "type": "event",
				"tick": e.Tick, "event": generic,
			})
		}
	}
}

// ---- errors -----------------------------------------------------------------
var (
	ErrUnauthorized   = errors.New("match: not authorized")
	ErrUnboundSession = errors.New("match: session is not bound to a player")
	ErrStaleSequence  = errors.New("match: stale action sequence")
	// ErrInvalidSequence is a protocol violation rather than a stale replay: the
	// sequence is outside the representable range, so accepting it would either
	// be rejected as stale forever (0) or overflow the next-sequence hint and
	// wedge the session (MaxInt64).
	ErrInvalidSequence = errors.New("match: action sequence out of range")
)

func newMatchID() (string, error) { return newID("m_") }

func newID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RateLimiter bounds actions per session on a sliding window. It is a
// per-connection abuse guard, not gameplay logic.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

// NewRateLimiter builds a limiter allowing `limit` events per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time), now: time.Now}
}

// Allow reports whether key may act now, recording the attempt when it may.
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-r.window)
	kept := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}
	r.hits[key] = append(kept, now)
	return true
}
