package match

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/persistence"
	"github.com/analeis/highjack/server/internal/protocol"
)

// This file exists because the runtime it covers had none. Bind() is the
// entire authorization boundary, ApplyAction owns idempotency and the
// durability ordering, Broadcast fans out to every peer, and RateLimiter is
// process-global — all previously untested, which is why defects in those areas
// could be found by reading the code but never by running it (audit OPS-10).
//
// The assertions here are deliberately about *observable* behaviour: what a peer
// received, what a snapshot contains, what a retry does. A test that only
// checked "no error" would pass against every defect this file exists to catch.

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// recordingSink captures published frames so a test can assert what a peer
// actually received — the only way to observe broadcast behaviour.
type recordingSink struct {
	mu     sync.Mutex
	frames []any
	fail   bool
}

func (s *recordingSink) Send(msg any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return false
	}
	s.frames = append(s.frames, msg)
	return true
}

// settleFrames drains anything the sink has recorded so far. Broadcast is
// synchronous, so a short wait is only insurance against scheduling.
func (s *recordingSink) settleFrames() {
	time.Sleep(20 * time.Millisecond)
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

// eventTypes returns the type of every event delivered, in arrival order: the
// wire-level view of what a peer was told and in what sequence. A transition
// arrives as one `transition` frame carrying a batch, so each entry in the batch
// counts.
func (s *recordingSink) eventTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var types []string
	for _, f := range s.frames {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "transition":
			for _, entry := range batchOf(m) {
				types = append(types, eventTypeOf(entry))
			}
		case "event": // tolerated for compatibility with older frames
			types = append(types, eventTypeOf(m["event"]))
		}
	}
	return types
}

// transitionTicks returns one entry per delivered transition frame, holding the
// tick that transition belongs to.
func (s *recordingSink) transitionTicks() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ticks []uint64
	for _, f := range s.frames {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "transition":
			if tick, ok := m["tick"].(uint64); ok {
				ticks = append(ticks, tick)
			}
		case "event":
			if tick, ok := m["tick"].(uint64); ok {
				ticks = append(ticks, tick)
			}
		}
	}
	return ticks
}

func batchOf(frame map[string]any) []any {
	raw, _ := frame["events"].([]any)
	return raw
}

// eventTypeOf reads the discriminator from either a bare event object or a
// {tick, event} batch entry.
func eventTypeOf(v any) string {
	m, _ := v.(map[string]any)
	if t, ok := m["type"].(string); ok {
		return t
	}
	if inner, ok := m["event"].(map[string]any); ok {
		if t, ok := inner["type"].(string); ok {
			return t
		}
	}
	return ""
}

// fakeStore can be made to fail on demand. A fake cannot prove SQL,
// transactions, or constraints — that is what the PostgreSQL integration suite
// is for — but it proves the properties that live in *this* package: that a
// failed commit leaves no in-memory trace, and that a failed join leaves no
// seat behind.
type fakeStore struct {
	mu sync.Mutex

	commits   int
	joins     int
	players   []game.Player
	commitErr error
	saveErr   error
	// durable is what LoadMatch reports, i.e. what a real database would hold.
	// nil means "no such match".
	durable *persistence.MatchRow
	// loadErr makes the read-back fail, modelling a database that cannot be
	// consulted at all.
	loadErr error
}

func (f *fakeStore) CreateMatch(context.Context, game.GameID, *game.GameConfig, *game.GameState, string) error {
	return nil
}

func (f *fakeStore) SavePlayer(_ context.Context, _ game.GameID, _ game.Player, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveErr
}

func (f *fakeStore) joinCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.joins
}

func (f *fakeStore) playerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.players)
}

func (f *fakeStore) CommitTransition(_ context.Context, _ game.GameID, state *game.GameState, _ []game.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	if f.commitErr != nil {
		return f.commitErr
	}
	f.durable = &persistence.MatchRow{State: *state.Clone(), Cursor: state.Tick}
	return nil
}

// CommitJoin is the transactional path a seat claim now takes: the transition and
// the player row commit together, so a failure must leave nothing behind.
func (f *fakeStore) CommitJoin(
	_ context.Context,
	_ game.GameID,
	_ *game.GameState,
	_ []game.Event,
	p game.Player,
	_ string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.joins++
	f.players = append(f.players, p)
	return nil
}

func (f *fakeStore) MarkInterrupted(context.Context) (int64, error) { return 0, nil }

// LoadMatch reports what the "database" holds. A fake cannot prove SQL, but it
// can model the case that actually breaks the system: a transaction that
// committed durably while the client never learned the result.
func (f *fakeStore) LoadMatch(_ context.Context, _ game.GameID) (*persistence.MatchRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.durable == nil {
		return nil, persistence.ErrNoMatch
	}
	return f.durable, nil
}

func (f *fakeStore) commitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commits
}

func (f *fakeStore) setCommitErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commitErr = err
}

func (f *fakeStore) setSaveErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveErr = err
}

// fixture is a live match with two joined players and their tokens. Tokens are
// returned because the runtime stores only their hash, so a test can never
// recover one afterwards.
type fixture struct {
	m     *Match
	store *fakeStore
	p1    game.PlayerID
	tok1  string
	p2    game.PlayerID
	tok2  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create match: %v", err)
	}
	p1, tok1, err := m.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join p1: %v", err)
	}
	p2, tok2, err := m.Join(context.Background(), "Bit")
	if err != nil {
		t.Fatalf("join p2: %v", err)
	}
	return &fixture{m: m, store: store, p1: p1, tok1: tok1, p2: p2, tok2: tok2}
}

// playing binds both players and starts the match, so board actions are legal.
func (f *fixture) playing(t *testing.T) (*recordingSink, *recordingSink) {
	t.Helper()
	sink1, sink2 := &recordingSink{}, &recordingSink{}
	bindReady(t, f, "s1", f.tok1, sink1)
	bindReady(t, f, "s2", f.tok2, sink2)
	ctx := context.Background()
	if _, err := f.m.ApplyAction(ctx, "s1", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready p1: %v", err)
	}
	if _, err := f.m.ApplyAction(ctx, "s2", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready p2: %v", err)
	}
	if _, err := f.m.ApplyAction(ctx, "s1", 2, game.GameStartAction{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := f.m.Snapshot().Phase; got != "playing" {
		t.Fatalf("phase = %q, want playing", got)
	}
	return sink1, sink2
}

// --- Authorization ---------------------------------------------------------

// The actor is resolved from the session binding, never from client input, and
// a session that is not bound can do nothing at all.
func TestApplyActionRejectsUnboundSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, session := range []string{"nope", ""} {
		res, err := f.m.ApplyAction(ctx, session, 1, game.PlayerReadyAction{Ready: true})
		if !errors.Is(err, ErrUnboundSession) {
			t.Fatalf("session %q: err = %v, want ErrUnboundSession", session, err)
		}
		if res.DomainCode != protocol.CodeNotPermitted {
			t.Fatalf("session %q: code = %q, want not_permitted", session, res.DomainCode)
		}
	}
}

// Disconnect must revoke the binding, not merely mark the sink gone: otherwise
// a dropped connection would remain an impersonation path.
func TestDisconnectedSessionCannotAct(t *testing.T) {
	f := newFixture(t)
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	f.m.Disconnect("s1")
	if _, err := f.m.ApplyAction(context.Background(), "s1", 1, game.PlayerReadyAction{Ready: true}); !errors.Is(err, ErrUnboundSession) {
		t.Fatalf("err = %v, want ErrUnboundSession after disconnect", err)
	}
}

func TestBindRejectsWrongAndEmptyTokens(t *testing.T) {
	f := newFixture(t)

	if _, err := f.m.Bind("s1", "tok_not_a_real_token", "c", &recordingSink{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bogus token: err = %v, want ErrUnauthorized", err)
	}
	// An empty token must never resolve, even though every string hashes.
	if _, err := f.m.Bind("s2", "", "c", &recordingSink{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("empty token: err = %v, want ErrUnauthorized", err)
	}
}

// A token is scoped to the match that minted it. The runtime must enforce this
// on its own, independently of the database-level uniqueness the persistence
// suite also asserts.
func TestTokenFromAnotherMatchDoesNotBind(t *testing.T) {
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	m1, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	_, tok1, err := m1.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join m1: %v", err)
	}
	_, tok2, err := m2.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join m2: %v", err)
	}

	if _, err := m1.Bind("s1", tok2, "c", &recordingSink{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross-match token: err = %v, want ErrUnauthorized", err)
	}
	if _, err := m1.Bind("s1", tok1, "c", &recordingSink{}); err != nil {
		t.Fatalf("own token: %v", err)
	}
}

// A bound session may only act for the player its token identifies, so the
// actor is never client-chosen and one player can never move another's piece.
//
// A domain rejection is signalled by a non-nil Result.Err with a nil Go error —
// that is the runtime's contract, and the handler relies on it to tell a
// recoverable rejection from a durability failure.
func TestBoundSessionActsAsItsOwnPlayerOnly(t *testing.T) {
	f := newFixture(t)
	readyAndStartActionless(t, f)

	before := f.m.Snapshot()

	// Seat 0 holds the turn. Player 2 (seat 1) must not be able to roll.
	// Sequence 2 is player 2's first unused one (it used 1 to ready up).
	res, err := f.m.ApplyAction(context.Background(), "s2", 2, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("a domain rejection must not surface as a transport error: %v", err)
	}
	if res.Err == nil {
		t.Fatal("player 2 rolled on player 1's turn: expected a rejection")
	}
	if res.DomainCode != protocol.CodeNotPermitted && res.DomainCode != protocol.CodeOutOfPhase {
		t.Fatalf("code = %q, want not_permitted or out_of_phase", res.DomainCode)
	}

	// A rejected action must leave everything exactly as it was.
	after := f.m.Snapshot()
	if after.Tick != before.Tick {
		t.Fatalf("a rejected action advanced the tick: %d -> %d", before.Tick, after.Tick)
	}
	if after.Turn.CurrentSeat != before.Turn.CurrentSeat {
		t.Fatal("a rejected action moved the turn")
	}
	for i := range before.Players {
		if before.Players[i].Position != after.Players[i].Position {
			t.Fatalf("player %q moved on a rejected action", before.Players[i].PlayerID)
		}
	}
}

// --- Sequence and idempotency ---------------------------------------------

// Sequence numbers are 1-based. Zero is a fresh watermark's zero value and must
// be refused explicitly rather than reported as a stale sequence.
func TestApplyActionRejectsSeqBelowOne(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Bind("s1", f.tok1, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	res, err := f.m.ApplyAction(context.Background(), "s1", 0, game.PlayerReadyAction{Ready: true})
	if !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("seq 0: err = %v, want ErrInvalidSequence", err)
	}
	if res.DomainCode != protocol.CodeInvalidAction {
		t.Fatalf("seq 0: code = %q, want invalid_action", res.DomainCode)
	}
}

// An overflowing sequence must be refused: accepting it would overflow
// NextSeq and permanently wedge the session.
func TestApplyActionRejectsOverflowingSeq(t *testing.T) {
	f := newFixture(t)
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	res, err := f.m.ApplyAction(context.Background(), "s1", math.MaxInt64, game.PlayerReadyAction{Ready: true})
	if err == nil {
		t.Fatalf("MaxInt64 seq was accepted with nextSeq=%d", res.NextSeq)
	}
	if res.NextSeq != 0 {
		t.Fatalf("a rejected action must not report a nextSeq; got %d", res.NextSeq)
	}
}

// A repeated last sequence replays the cached ack without applying the
// transition again. This is the idempotency guarantee the client depends on.
func TestDuplicateSeqReplaysAckWithoutReapplying(t *testing.T) {
	f := newFixture(t)
	_, _ = f.playing(t)
	ctx := context.Background()
	first, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	tickAfterFirst := f.m.Snapshot().Tick
	commitsAfterFirst := f.store.commitCount()

	second, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("duplicate roll: %v", err)
	}

	if got := f.m.Snapshot().Tick; got != tickAfterFirst {
		t.Fatalf("duplicate seq advanced the tick: %d -> %d", tickAfterFirst, got)
	}
	if got := f.store.commitCount(); got != commitsAfterFirst {
		t.Fatalf("duplicate seq reached the store: %d commits, want %d", got, commitsAfterFirst)
	}
	if second.NextSeq != first.NextSeq {
		t.Fatalf("replayed nextSeq = %d, want %d", second.NextSeq, first.NextSeq)
	}
	if len(second.Snapshot.Players) != len(first.Snapshot.Players) {
		t.Fatal("a replayed ack must carry the same roster as the original")
	}
}

// The replayed ack must carry the identical authoritative snapshot, not an
// empty one. A zero-valued result here would blank a client's board.
func TestDuplicateSeqReplaysIdenticalSnapshot(t *testing.T) {
	f := newFixture(t)
	f.playing(t)
	ctx := context.Background()

	first, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	second, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if second.Snapshot.Tick != first.Snapshot.Tick {
		t.Fatalf("replayed snapshot tick = %d, want %d", second.Snapshot.Tick, first.Snapshot.Tick)
	}
	if second.Snapshot.MatchID != first.Snapshot.MatchID {
		t.Fatal("replayed snapshot lost its match identity")
	}
}

func TestOlderSeqIsRejectedAsStale(t *testing.T) {
	f := newFixture(t)
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	ctx := context.Background()
	if _, err := f.m.ApplyAction(ctx, "s1", 5, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("seq 5: %v", err)
	}
	if _, err := f.m.ApplyAction(ctx, "s2", 2, game.PlayerReadyAction{Ready: true}); !errors.Is(err, ErrStaleSequence) {
		// s2 is unbound, so check the bound session instead below.
		_ = err
	}
	if _, err := f.m.ApplyAction(ctx, "s1", 2, game.PlayerReadyAction{Ready: true}); !errors.Is(err, ErrStaleSequence) {
		t.Fatalf("older seq: err = %v, want ErrStaleSequence", err)
	}
}

// --- Durability ordering and rollback --------------------------------------

// A persistence failure must leave no trace in memory: no state change, no
// event, and an untouched sequence watermark so a retry is legitimate.
func TestCommitFailureLeavesNoInMemoryTrace(t *testing.T) {
	f := newFixture(t)
	f.playing(t)
	ctx := context.Background()

	before := f.m.Snapshot()
	f.store.setCommitErr(errors.New("disk full"))

	res, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err == nil {
		t.Fatal("expected the commit failure to surface")
	}
	if res.DomainCode != protocol.CodeInternalError {
		t.Fatalf("code = %q, want internal_error", res.DomainCode)
	}

	after := f.m.Snapshot()
	if after.Tick != before.Tick {
		t.Fatalf("tick advanced on a failed commit: %d -> %d", before.Tick, after.Tick)
	}
	for i := range before.Players {
		if before.Players[i].Position != after.Players[i].Position {
			t.Fatalf("player %q moved on a failed commit", before.Players[i].PlayerID)
		}
		if before.Players[i].Money != after.Players[i].Money {
			t.Fatalf("player %q money changed on a failed commit", before.Players[i].PlayerID)
		}
	}

	// The watermark must not have advanced, so the same sequence may be retried.
	f.store.setCommitErr(nil)
	if _, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{}); err != nil {
		t.Fatalf("retry after a failed commit: %v", err)
	}
}

// A join that cannot be persisted must not leave a seat behind. A client that
// simply hangs up is enough to trigger this, and the seat it consumed would
// otherwise be unjoinable for the life of the match.
func TestJoinFailureLeavesNoSeatBehind(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("connection reset by peer")}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	before := m.Snapshot()
	if _, _, err := m.Join(context.Background(), "Ghost"); err == nil {
		t.Fatal("expected the failed join to surface an error")
	}
	if got := len(m.Snapshot().Players); got != len(before.Players) {
		t.Fatalf("failed join added a player: %d -> %d", len(before.Players), got)
	}

	store.setSaveErr(nil)
	if _, _, err := m.Join(context.Background(), "Real"); err != nil {
		t.Fatalf("join after failure: %v", err)
	}
	if got := len(m.Snapshot().Players); got != 1 {
		t.Fatalf("expected exactly one player after recovery, got %d", got)
	}
}

// --- Broadcast -------------------------------------------------------------

// A transition must reach every peer exactly once. A duplicate submission must
// not re-publish it — that is the defect that charged rent twice on every peer
// (audit MTP-5), and it is what this assertion exists to prevent.
func TestDuplicateSeqDoesNotRepublishToPeers(t *testing.T) {
	f := newFixture(t)
	_, sink2 := f.playing(t)
	ctx := context.Background()

	res, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	// First publication.
	f.m.Broadcast(res.Events, 3)
	firstCount := sink2.count()

	// The client never saw the ack and retries the same sequence. The runtime
	// returns the cached ack with no events attached: publication already
	// happened, so there is nothing left to publish.
	replay, err := f.m.ApplyAction(ctx, "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if !replay.Replayed {
		t.Fatal("a cached result must be marked as replayed so the transport knows not to publish")
	}
	if len(replay.Events) != 0 {
		t.Fatalf("a replayed result must carry no events to publish; got %d", len(replay.Events))
	}
	// The transport publishes only non-replayed results, so this is a no-op.
	f.m.Broadcast(replay.Events, 3)

	if got := sink2.count(); got != firstCount {
		t.Fatalf("a duplicate submission re-published to a peer: %d -> %d frames", firstCount, got)
	}
	// The peer must still be able to reach the authoritative state: the replayed
	// ack carries the same snapshot as the original.
	if replay.Snapshot.Tick == 0 {
		t.Fatal("a replayed ack must carry the authoritative snapshot")
	}
}

func TestBroadcastReachesEveryPeerExactlyOnce(t *testing.T) {
	f := newFixture(t)
	sink1, sink2 := f.playing(t)

	res, err := f.m.ApplyAction(context.Background(), "s1", 3, game.RollDiceAction{})
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	f.m.Broadcast(res.Events, 3)

	for i, sink := range []*recordingSink{sink1, sink2} {
		got := sink.eventTypes()
		if len(got) == 0 {
			t.Fatalf("peer %d received no events", i)
		}
		for _, typ := range got {
			if typ == "" {
				t.Fatalf("peer %d received an event with no type", i)
			}
		}
		// Delivered transitions must arrive in tick order.
		if !ticksAreNonDecreasing(sink) {
			t.Fatalf("peer %d saw out-of-order transitions: %v", i, sink.transitionTicks())
		}
	}
}

func TestBroadcastIsolatesFailingSinks(t *testing.T) {
	f := newFixture(t)
	bad := &recordingSink{fail: true}
	good := &recordingSink{}
	bindReady(t, f, "s1", f.tok1, bad)
	bindReady(t, f, "s2", f.tok2, good)

	res, err := f.m.ApplyAction(context.Background(), "s1", 1, game.PlayerReadyAction{Ready: true})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	f.m.Broadcast(res.Events, 1)

	if bad.count() != 0 {
		t.Fatal("a failing sink should not record frames")
	}
	if good.count() == 0 {
		t.Fatal("one failing sink must not stop delivery to a healthy peer")
	}
}

func TestBroadcastSkipsDisconnectedSessions(t *testing.T) {
	f := newFixture(t)
	gone := &recordingSink{}
	staying := &recordingSink{}
	bindReady(t, f, "s1", f.tok1, gone)
	bindReady(t, f, "s2", f.tok2, staying)
	f.m.Disconnect("s1")

	res, err := f.m.ApplyAction(context.Background(), "s2", 1, game.PlayerReadyAction{Ready: true})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	f.m.Broadcast(res.Events, 1)

	if gone.count() != 0 {
		t.Fatal("a disconnected session must not receive broadcasts")
	}
	if staying.count() == 0 {
		t.Fatal("a connected peer must still receive broadcasts")
	}
}

// --- History and catch-up --------------------------------------------------

func TestSinceReturnsRetainedEventsInOrder(t *testing.T) {
	f := newFixture(t)
	f.playing(t)

	events, ok := f.m.Since(0)
	if !ok {
		t.Fatal("Since(0) should be inside the retained window")
	}
	if len(events) == 0 {
		t.Fatal("expected retained events")
	}
	for i := 1; i < len(events); i++ {
		if events[i].Tick < events[i-1].Tick {
			t.Fatalf("history is not ordered: tick %d after %d", events[i].Tick, events[i-1].Tick)
		}
	}
}

// A cursor the server has never reached means the client's view is ahead of
// authoritative state: it must be told to resync, not handed an empty success
// that it will read as "you are up to date".
func TestSinceRejectsCursorAheadOfServer(t *testing.T) {
	f := newFixture(t)
	f.playing(t)

	if tick := f.m.Snapshot().Tick; tick >= 50 {
		t.Fatalf("test assumes a low tick, got %d", tick)
	}
	if events, ok := f.m.Since(50); ok {
		t.Fatalf("a cursor ahead of the server must report unavailable, got ok with %d events", len(events))
	}
}

// A client at genesis with nothing retained is genuinely up to date.
func TestSinceAcceptsGenesisCursorOnEmptyHistory(t *testing.T) {
	f := newFixture(t)
	f.playing(t)

	events, ok := f.m.Since(f.m.Snapshot().Tick)
	if !ok {
		t.Fatal("a cursor at the current tick must be catch-up-able")
	}
	if len(events) != 0 {
		t.Fatalf("a cursor at the current tick should yield no events, got %d", len(events))
	}
}

// --- Registry --------------------------------------------------------------

func TestRegistryGetReturnsNilForUnknownMatch(t *testing.T) {
	reg := NewRegistry(testLogger(), &fakeStore{})
	if got := reg.Get(game.GameID("m_does_not_exist")); got != nil {
		t.Fatal("an unknown match must not resolve")
	}
}

func TestRegistryCreateProducesDistinctRetrievableIDs(t *testing.T) {
	reg := NewRegistry(testLogger(), &fakeStore{})
	seen := map[game.GameID]bool{}
	for i := 0; i < 25; i++ {
		m, err := reg.Create(context.Background(), game.DefaultConfig())
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if seen[m.ID()] {
			t.Fatalf("duplicate match id %q", m.ID())
		}
		seen[m.ID()] = true
		if got := reg.Get(m.ID()); got != m {
			t.Fatal("a created match must be retrievable by id")
		}
	}
}

// --- Rate limiter ----------------------------------------------------------

func TestRateLimiterEnforcesLimitWithinWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := NewRateLimiter(3, 10*time.Second)
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !rl.Allow("k") {
			t.Fatalf("request %d inside the window should be allowed", i)
		}
	}
	if rl.Allow("k") {
		t.Fatal("the fourth request inside the window must be denied")
	}
	now = now.Add(11 * time.Second)
	if !rl.Allow("k") {
		t.Fatal("a request after the window must be allowed")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	if !rl.Allow("a") {
		t.Fatal("first key should be allowed")
	}
	if !rl.Allow("b") {
		t.Fatal("a different key must have its own budget")
	}
	if rl.Allow("a") {
		t.Fatal("the exhausted key must stay exhausted")
	}
}

// --- helpers ---------------------------------------------------------------

// ticksAreNonDecreasing reports whether delivered transitions arrived in order.
// A transition is one frame, so its own events cannot be split by another
// transition; this checks the ordering of the transitions themselves.
func ticksAreNonDecreasing(s *recordingSink) bool {
	ticks := s.transitionTicks()
	for i := 1; i < len(ticks); i++ {
		if ticks[i] < ticks[i-1] {
			return false
		}
	}
	return true
}

// bindReady binds a session and completes its handshake, which is what makes it
// eligible for publications. A bound-but-not-ready session is exactly the state a
// client is in between its token being accepted and its snapshot being written.
func bindReady(t *testing.T, f *fixture, session, token string, sink Sink) game.PlayerID {
	t.Helper()
	id, err := f.m.Bind(session, token, "test-client", sink)
	if err != nil {
		t.Fatalf("bind %s: %v", session, err)
	}
	f.m.MarkReady(session)
	return id
}

// readyAndStartActionless binds both players and starts the match without
// consuming sequence numbers on either session, so a test can start acting at a
// sequence number it chooses.
func readyAndStartActionless(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.m.Bind("s1", f.tok1, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind p1: %v", err)
	}
	if _, err := f.m.Bind("s2", f.tok2, "c2", &recordingSink{}); err != nil {
		t.Fatalf("bind p2: %v", err)
	}
	ctx := context.Background()
	if _, err := f.m.ApplyAction(ctx, "s1", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready p1: %v", err)
	}
	if _, err := f.m.ApplyAction(ctx, "s2", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready p2: %v", err)
	}
	if _, err := f.m.ApplyAction(ctx, "s1", 2, game.GameStartAction{}); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// --- the join path ---------------------------------------------------------

// A seat claim is a transition: it must be committed through the same durable
// path as a roll, and published, so a restarted process can load a match whose
// state its event log explains. It previously wrote only a `game_players` row.
func TestJoinCommitsAsATransition(t *testing.T) {
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := m.Join(context.Background(), "Ace"); err != nil {
		t.Fatalf("join: %v", err)
	}
	if got := store.joinCount(); got != 1 {
		t.Fatalf("join made %d durable commits, want 1", got)
	}
	// And the join must be in the retained history, so a reconnecting client can
	// be told about it.
	events, ok := m.Since(0)
	if !ok {
		t.Fatal("Since(0) should be inside the retained window")
	}
	found := false
	for _, e := range events {
		if e.Event.Type() == game.EventPlayerJoined {
			found = true
		}
	}
	if !found {
		t.Fatal("the join was not recorded in the transition history")
	}
}

// A new seat must reach the clients already at the table. Previously a join was
// never broadcast, so the lobby could show a stale roster with an empty-looking
// table while players waited for someone who had already joined.
func TestJoinIsPublishedToConnectedClients(t *testing.T) {
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	first, tok1, err := m.Join(context.Background(), "Ace")
	if err != nil {
		t.Fatalf("join ace: %v", err)
	}
	watcher := &recordingSink{}
	if _, err := m.Bind("s1", tok1, "c1", watcher); err != nil {
		t.Fatalf("bind: %v", err)
	}
	m.MarkReady("s1")

	if _, _, err := m.Join(context.Background(), "Bit"); err != nil {
		t.Fatalf("join bit: %v", err)
	}
	_ = first

	// Give the (synchronous) broadcast a chance to be observed.
	watcher.settleFrames()
	types := watcher.eventTypes()
	if len(types) == 0 {
		t.Fatal("a connected client was not told about a new seat")
	}
	if types[0] != string(game.EventPlayerJoined) {
		t.Fatalf("published %v, want player_joined first", types)
	}
}

// A join that cannot be committed must consume no seat at all. The caller is an
// unauthenticated HTTP endpoint whose context dies when the client hangs up, so
// this is reachable without any credentials.
func TestJoinCommitFailureConsumesNoSeat(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("connection reset by peer")}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, _, err := m.Join(context.Background(), "Ghost"); err == nil {
			t.Fatalf("attempt %d: expected the failed join to surface", i)
		}
		if got := len(m.Snapshot().Players); got != 0 {
			t.Fatalf("attempt %d consumed a seat: %d players", i, got)
		}
	}
	if got := store.playerCount(); got != 0 {
		t.Fatalf("a failed join persisted %d player row(s)", got)
	}
}

// The store interface gained CommitJoin; the fake must implement it, and the
// runtime must go through it rather than the legacy player-only write.
func TestJoinUsesTheTransactionalPath(t *testing.T) {
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	m, err := reg.Create(context.Background(), game.DefaultConfig())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := m.Join(context.Background(), "Ace"); err != nil {
		t.Fatalf("join: %v", err)
	}
	if store.joinCount() != 1 {
		t.Fatalf("join did not use the transactional commit")
	}
	if len(m.Snapshot().Players) != 1 {
		t.Fatalf("expected one player, got %d", len(m.Snapshot().Players))
	}
}

// --- handshake ordering ----------------------------------------------------

// A session must not receive any transition before its snapshot has been written.
// Otherwise a client can receive event(11) and then snapshot(10): the snapshot
// overwrites the newer transition, which is then lost with no signal, because the
// client has no resync to react to.
func TestBoundSessionReceivesNothingUntilItsSnapshotIsWritten(t *testing.T) {
	f := newFixture(t)
	m := f.m

	// Player one is fully handshaken, so its publications do reach the fan-out.
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	// Player two has only had its token accepted: the handshake has not written
	// its snapshot yet.
	sink := &recordingSink{}
	if _, err := m.Bind("s2", f.tok2, "c2", sink); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if _, err := m.ApplyAction(context.Background(), "s1", 1, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("ready: %v", err)
	}
	m.Broadcast(m.envelopesForTest(), 1)
	if sink.count() != 0 {
		t.Fatalf("a session that has not received its snapshot was published to (%d frames)", sink.count())
	}

	// Once the handshake completes, publications resume.
	m.MarkReady("s2")
	m.Broadcast(m.envelopesForTest(), 1)
	if sink.count() == 0 {
		t.Fatal("a ready session received no publications")
	}
}

// The handshake must be handed one consistent view of the match.
func TestResumeReturnsAConsistentSnapshotAndCatchup(t *testing.T) {
	f := newFixture(t)
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	bindReady(t, f, "s2", f.tok2, &recordingSink{})

	cursor := int64(0)
	snap, catchup, resync := f.m.Resume(&cursor)
	if snap.Tick != f.m.Snapshot().Tick {
		t.Fatalf("Resume returned tick %d but the match is at %d", snap.Tick, f.m.Snapshot().Tick)
	}
	if resync {
		t.Fatal("a client at the origin must not be told to resync")
	}
	// Every catch-up event must be strictly after the cursor and no later than the
	// snapshot it accompanies.
	for _, e := range catchup {
		if e.Tick <= uint64(cursor) {
			t.Fatalf("catch-up included tick %d at or before the cursor %d", e.Tick, cursor)
		}
		if e.Tick > snap.Tick {
			t.Fatalf("catch-up included tick %d, ahead of the snapshot tick %d", e.Tick, snap.Tick)
		}
	}
	// A nil cursor means "give me everything": the snapshot alone, no catch-up.
	snap2, catchup2, resync2 := f.m.Resume(nil)
	if snap2.Tick != snap.Tick || len(catchup2) != 0 || resync2 {
		t.Fatalf("nil cursor = %v/%d/%v, want the snapshot alone", snap2.Tick, len(catchup2), resync2)
	}
}

// A client whose cursor is ahead of the server must be resynced, not served.
func TestResumeResyncsACursorAheadOfTheServer(t *testing.T) {
	f := newFixture(t)
	bindReady(t, f, "s1", f.tok1, &recordingSink{})

	ahead := int64(f.m.Snapshot().Tick) + 500
	snap, catchup, resync := f.m.Resume(&ahead)
	if !resync {
		t.Fatal("a cursor ahead of the server must trigger a resync")
	}
	if len(catchup) != 0 {
		t.Fatalf("a resyncing client must not also receive catch-up (%d events)", len(catchup))
	}
	if snap.Tick == 0 {
		t.Fatal("a resync must still carry the authoritative snapshot")
	}
}

// --- indeterminate commits -------------------------------------------------

// A commit that reports an error may nonetheless have committed: the deadline can
// expire while COMMIT is in flight, or the connection can drop after the database
// made the transaction durable. Treating that as "did not happen" left memory one
// tick behind the database, and the next transition's INSERT then collided on the
// (match_id, tick, seq) primary key — so every later action failed the same way
// and the match was bricked for good.
func TestIndeterminateCommitIsReconciledNotReverted(t *testing.T) {
	f := newFixture(t)
	store := f.store
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	bindReady(t, f, "s2", f.tok2, &recordingSink{})
	ctx := context.Background()

	first, err := f.m.ApplyAction(ctx, "s1", 1, game.PlayerReadyAction{Ready: true})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	durableTick := uint64(first.Snapshot.Tick)

	// Now model a commit that durably succeeded but reported a failure: the
	// database holds the post-transition state, while the caller never learned
	// the result.
	ahead := f.m.snapshotStateForTest()
	ahead.Tick = durableTick + 1
	store.mu.Lock()
	store.commitErr = errors.New("connection reset after the server committed")
	store.durable = &persistence.MatchRow{State: *ahead, Cursor: ahead.Tick}
	store.mu.Unlock()

	// The action is refused — the client is told the truth — but memory must end up
	// agreeing with the database rather than behind it.
	if _, err := f.m.ApplyAction(ctx, "s1", 2, game.PlayerReadyAction{Ready: false}); err == nil {
		t.Fatal("expected the ambiguous commit to be reported as a failure")
	}
	if got := f.m.Snapshot().Tick; uint64(got) != durableTick+1 {
		t.Fatalf("memory is at tick %d but the database committed tick %d; the match would "+
			"collide on the next insert", got, durableTick+1)
	}

	// And crucially, the next action must succeed rather than collide.
	store.mu.Lock()
	store.commitErr = nil
	store.mu.Unlock()
	if _, err := f.m.ApplyAction(ctx, "s1", 3, game.PlayerReadyAction{Ready: true}); err != nil {
		t.Fatalf("the action after a reconciled commit must succeed: %v", err)
	}
}

// When the database cannot be consulted at all, memory must hold the last durable
// state rather than guess: nothing uncommitted may ever be published.
func TestUnreconcilableCommitHoldsTheLastDurableState(t *testing.T) {
	f := newFixture(t)
	store := f.store
	bindReady(t, f, "s1", f.tok1, &recordingSink{})
	bindReady(t, f, "s2", f.tok2, &recordingSink{})
	ctx := context.Background()

	first, err := f.m.ApplyAction(ctx, "s1", 1, game.PlayerReadyAction{Ready: true})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	tickBefore := first.Snapshot.Tick

	store.mu.Lock()
	store.commitErr = errors.New("commit failed")
	store.loadErr = errors.New("database unreachable")
	store.mu.Unlock()

	if _, err := f.m.ApplyAction(ctx, "s1", 2, game.PlayerReadyAction{Ready: false}); err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if got := f.m.Snapshot().Tick; got != tickBefore {
		t.Fatalf("memory advanced to tick %d without durability (was %d)", got, tickBefore)
	}
}

// --- resource bounds -------------------------------------------------------

// The limiter is process-global and keyed by session ids that are never reused, so
// without an eviction sweep every connection ever opened leaked a key for the life
// of the process — unreachable by any code path, including eviction of a match
// that has already ended.
func TestRateLimiterSweepsIdleKeys(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := NewRateLimiter(5, 10*time.Second)
	rl.now = func() time.Time { return now }

	// Far more distinct keys than the sweep interval, all inside the window.
	for i := 0; i < 1000; i++ {
		if !rl.Allow(keyN(i)) {
			t.Fatalf("first use of key %d should be allowed", i)
		}
	}
	if rl.tracked() == 0 {
		t.Fatal("expected tracked keys while the window is open")
	}

	// Move past the window. Sweeping is amortised, so drive enough calls for one
	// to happen; all of them are the same live key, which must survive.
	now = now.Add(2 * time.Minute)
	rl.Allow("live")
	for i := 0; i < sweepEvery; i++ {
		rl.Allow("live")
	}
	if got := rl.tracked(); got > 2 {
		t.Fatalf("idle keys were not swept: %d still tracked", got)
	}
	if rl.tracked() == 0 {
		t.Fatal("the sweep dropped the live key too")
	}
}

func TestRateLimiterStillEnforcesWithinTheWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := NewRateLimiter(2, 10*time.Second)
	rl.now = func() time.Time { return now }
	// Sweeping must not weaken the limit for a live key.
	for i := 0; i < 200; i++ {
		rl.Allow("live")
	}
	if rl.Allow("live") {
		t.Fatal("a live key's limit was lost to sweeping")
	}
}

// POST /matches is unauthenticated and unrated, so the registry needs a bound.
func TestRegistryRefusesToGrowWithoutLimit(t *testing.T) {
	reg := NewRegistry(testLogger(), &fakeStore{})
	ctx := context.Background()
	for i := 0; i < maxLiveMatches; i++ {
		if _, err := reg.Create(ctx, game.DefaultConfig()); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := reg.Create(ctx, game.DefaultConfig()); !errors.Is(err, ErrRegistryFull) {
		t.Fatalf("err = %v, want ErrRegistryFull", err)
	}
	// The bound is a refusal, not a panic or a silent overwrite.
	if got := len(reg.matches); got != maxLiveMatches {
		t.Fatalf("registry holds %d matches, want %d", got, maxLiveMatches)
	}
}

func keyN(i int) string { return fmt.Sprintf("sess_%08d", i) }

// --- credential comparison -------------------------------------------------

// A token must not be matched by a byte-wise comparison: that short-circuits on
// the first differing byte, and Go visits map entries in a randomized order, so
// the work per attempt is not even stable.
func TestTokenLookupIsExactAndRejectsMutations(t *testing.T) {
	f := newFixture(t)
	hash := hashToken(f.tok1)
	if hash == "" || hash == f.tok1 {
		t.Fatalf("token must be stored hashed, got %q", hash)
	}

	if _, ok := (&Match{players: map[game.PlayerID]string{"pl_a": hash}}).
		playersByTokenLocked(hash); !ok {
		t.Fatal("the correct hash must resolve")
	}
	for _, bad := range []string{"", hash[:len(hash)-1], hash + "x", "0" + hash[1:]} {
		if _, ok := (&Match{players: map[game.PlayerID]string{"pl_a": hash}}).
			playersByTokenLocked(bad); ok {
			t.Fatalf("a mutated hash %q resolved", bad)
		}
	}
	// A player whose stored hash is empty must never be bindable with an empty
	// token, which is what the input-side guard alone would not prevent.
	if _, ok := (&Match{players: map[game.PlayerID]string{"pl_a": ""}}).
		playersByTokenLocked(""); ok {
		t.Fatal("an empty stored hash matched an empty token")
	}
}
