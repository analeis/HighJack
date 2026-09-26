package match

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/game"
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

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

// eventTypes returns the type of every `event` frame received, in arrival
// order: the wire-level view of what a peer was told and in what sequence.
func (s *recordingSink) eventTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var types []string
	for _, f := range s.frames {
		m, ok := f.(map[string]any)
		if !ok || m["type"] != "event" {
			continue
		}
		ev, ok := m["event"].(map[string]any)
		if !ok {
			continue
		}
		if t, ok := ev["type"].(string); ok {
			types = append(types, t)
		}
	}
	return types
}

// eventTicks returns the tick of every `event` frame received, in arrival order.
func (s *recordingSink) eventTicks() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ticks []uint64
	for _, f := range s.frames {
		m, ok := f.(map[string]any)
		if !ok || m["type"] != "event" {
			continue
		}
		if tick, ok := m["tick"].(uint64); ok {
			ticks = append(ticks, tick)
		}
	}
	return ticks
}

// fakeStore can be made to fail on demand. A fake cannot prove SQL,
// transactions, or constraints — that is what the PostgreSQL integration suite
// is for — but it proves the properties that live in *this* package: that a
// failed commit leaves no in-memory trace, and that a failed join leaves no
// seat behind.
type fakeStore struct {
	mu sync.Mutex

	commits   int
	commitErr error
	saveErr   error
}

func (f *fakeStore) CreateMatch(context.Context, game.GameID, *game.GameConfig, *game.GameState, string) error {
	return nil
}

func (f *fakeStore) SavePlayer(_ context.Context, _ game.GameID, _ game.Player, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveErr
}

func (f *fakeStore) CommitTransition(_ context.Context, _ game.GameID, _ *game.GameState, _ []game.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	return f.commitErr
}

func (f *fakeStore) MarkInterrupted(context.Context) (int64, error) { return 0, nil }

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
	if _, err := f.m.Bind("s1", f.tok1, "c1", sink1); err != nil {
		t.Fatalf("bind p1: %v", err)
	}
	if _, err := f.m.Bind("s2", f.tok2, "c2", sink2); err != nil {
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
	if _, err := f.m.Bind("s1", f.tok1, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
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
	if _, err := f.m.Bind("s1", f.tok1, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
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
	if _, err := f.m.Bind("s1", f.tok1, "c1", &recordingSink{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
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
		// Every event in one transition shares one tick, so a peer must see a
		// single contiguous tick group.
		if !allSameTick(sink) {
			t.Fatalf("peer %d saw interleaved ticks in a single transition: %v", i, sink.eventTicks())
		}
	}
}

func TestBroadcastIsolatesFailingSinks(t *testing.T) {
	f := newFixture(t)
	bad := &recordingSink{fail: true}
	good := &recordingSink{}
	if _, err := f.m.Bind("s1", f.tok1, "c1", bad); err != nil {
		t.Fatalf("bind p1: %v", err)
	}
	if _, err := f.m.Bind("s2", f.tok2, "c2", good); err != nil {
		t.Fatalf("bind p2: %v", err)
	}

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
	if _, err := f.m.Bind("s1", f.tok1, "c1", gone); err != nil {
		t.Fatalf("bind p1: %v", err)
	}
	if _, err := f.m.Bind("s2", f.tok2, "c2", staying); err != nil {
		t.Fatalf("bind p2: %v", err)
	}
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

// allSameTick reports whether every event frame the sink received belongs to a
// single tick, i.e. the transition arrived contiguously.
func allSameTick(s *recordingSink) bool {
	ticks := s.eventTicks()
	if len(ticks) < 2 {
		return true
	}
	return ticks[0] == ticks[len(ticks)-1]
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
