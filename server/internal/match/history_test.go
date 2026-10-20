package match

import (
	"context"
	"fmt"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
)

// This file covers the retention window.
//
// A transition emits several events that all share one tick, and the tick is the
// transition's identity on the wire: a client applies a whole transition at once.
// The retained window must therefore never begin in the middle of a tick group.
//
// The runtime had two history-maintaining paths and they disagreed. The join path
// trimmed on tick boundaries, because the alternative is described in its own
// comment: cutting mid-group retains a *partial* transition, `Since()` then serves
// that partial group as a complete catch-up, and a client applies a purchase
// without its payment. The action path — the one every roll, purchase and turn
// change takes — trimmed by raw event count and could cut mid-group.
//
// Neither path had a test, so the correct behaviour was asserted only in a
// comment.
//
// The reference for "how many events did this tick produce" is the action result
// itself, not a published frame. The published frames are useless as a reference
// here: `Broadcast` withholds everything from a session that has not been marked
// ready, and `MarkReady` is called by the realtime handshake, so a match-package
// test never sees a frame at all. A first attempt at this test used the sink and
// therefore compared the window against an empty reference, which made it pass
// without asserting anything.
//
// The match's dice come from the root seed, so the size of each transition's
// event group is random unless the seed is pinned. A defect that only appears
// for some dice cannot be tested by luck, so the seeds below are fixed: the
// window is exercised across several of them, and the outcome is the same on
// every run.
//
// fixedSeeds are the root seeds the retention window is exercised against.
//
// They are arbitrary but fixed. The point is not any particular seed but that
// the group sizes they produce differ enough for the trim to land mid-group on
// at least one of them, which is what the assertion detects.
var fixedSeeds = []game.Seed{
	{0x01, 0x02, 0x03, 0x04},
	{0x11, 0x12, 0x13, 0x14},
	{0x21, 0x22, 0x23, 0x24},
	{0x31, 0x32, 0x33, 0x34},
	{0x41, 0x42, 0x43, 0x44},
	{0x51, 0x52, 0x53, 0x54},
	{0x61, 0x62, 0x63, 0x64},
	{0x71, 0x72, 0x73, 0x74},
}

// newFixtureWithSeed builds a match whose root seed is pinned, so the dice — and
// therefore the number of events in each transition — are deterministic.
//
// It mirrors `Registry.Create` rather than adding a seeded creation path to the
// runtime: the seeded variant is a test concern, and the production registry
// has no reason to expose one.
func newFixtureWithSeed(t *testing.T, seed game.Seed) *fixture {
	t.Helper()
	store := &fakeStore{}
	reg := NewRegistry(testLogger(), store)
	cfg := game.DefaultConfig()

	matchID := game.GameID("m_fixed_seed")
	engine, err := game.NewEngine(&cfg, seed, game.MatchRuleset{})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	state := engine.NewState(matchID)
	state.Board = game.NewBoardState(&cfg)
	hash, err := cfg.Hash()
	if err != nil {
		t.Fatalf("hash config: %v", err)
	}
	m := &Match{
		id: matchID, cfg: &cfg, hash: hash, seedHex: seed.Hex(), engine: engine,
		state: state, sessions: make(map[string]*session),
		players: make(map[game.PlayerID]string), store: store, log: testLogger(),
	}
	if err := store.CreateMatch(context.Background(), matchID, &cfg, state, seed.Hex()); err != nil {
		t.Fatalf("create match: %v", err)
	}
	reg.mu.Lock()
	reg.matches[matchID] = m
	reg.mu.Unlock()

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

// tickEventCounts returns, for every tick, how many events that tick produced.
func tickEventCounts(results []ActionResult) map[uint64]int {
	counts := map[uint64]int{}
	for _, res := range results {
		for _, e := range res.Events {
			counts[e.Tick]++
		}
	}
	return counts
}

// retainedCounts returns, for every tick the runtime retained, how many of that
// tick's events are still in the window.
func retainedCounts(m *Match) map[uint64]int {
	counts := map[uint64]int{}
	for _, e := range m.history {
		counts[e.Tick]++
	}
	return counts
}

// driveMatch plays a match forward until it ends or the iteration budget runs
// out, and returns the result of every action it applied.
//
// It follows the real turn flow: a roll either demands a purchase decision, ends
// the turn, or grants an extra roll on doubles, and `end_turn` is legal only
// once the turn is over. Each cycle is therefore the shortest legal path through
// a turn, which fills the retention window quickly.
func driveMatch(t testing.TB, f *fixture, iterations int) []ActionResult {
	t.Helper()
	var results []ActionResult
	seq := int64(3)
	ctx := context.Background()
	// A hard ceiling on the number of actions, so a turn flow that does not
	// advance fails the test instead of running until the harness kills it.
	const maxActions = 12000

	apply := func(action game.Action) {
		t.Helper()
		if len(results) >= maxActions {
			t.Fatalf("action ceiling of %d reached: the turn flow is not advancing", maxActions)
		}
		snap := f.m.Snapshot()
		session := "s1"
		if snap.Turn.CurrentSeat == 1 {
			session = "s2"
		}
		res, err := f.m.ApplyAction(ctx, session, seq, action)
		if err != nil {
			t.Fatalf("apply %T: %v", action, err)
		}
		if res.Err != nil {
			t.Fatalf("%T rejected: %v", action, res.Err)
		}
		results = append(results, res)
		seq++
	}

	for i := 0; i < iterations; i++ {
		snap := f.m.Snapshot()
		if snap.Phase != "playing" {
			break
		}
		// Play out exactly one turn. The break condition is the end of the turn,
		// not the end of the match: the match phase stays "playing" throughout,
		// so breaking on it would never fire.
		for ended := false; !ended; {
			snap = f.m.Snapshot()
			if snap.Phase != "playing" {
				break
			}
			switch snap.Turn.Phase {
			case "await_roll":
				apply(game.RollDiceAction{})
			case "await_buy_decision":
				apply(game.DeclineBuyAction{})
			default: // turn_over
				apply(game.EndTurnAction{})
				ended = true
			}
		}
	}
	return results
}

// TestTrimHistoryLandsOnATickBoundary tests the trim directly, with a history
// of uniform groups, so the invariant is checked without reference to the dice.
//
// The two tests above exercise the trim through a real match, which is what
// proves the *runtime* never serves a partial transition. This one pins the
// mechanism: given a history whose ticks are known, the trim must land on a
// group boundary every time.
func TestTrimHistoryLandsOnATickBoundary(t *testing.T) {
	const groupSize = 3
	const ticks = 500

	m := &Match{log: testLogger()}
	for tick := uint64(1); tick <= ticks; tick++ {
		for i := 0; i < groupSize; i++ {
			m.history = append(m.history, EventEnvelope{Tick: tick})
		}
	}
	total := uint64(ticks * groupSize)
	if uint64(len(m.history)) != total {
		t.Fatalf("built %d events, want %d", len(m.history), total)
	}

	m.trimHistoryLocked()

	if uint64(len(m.history)) > historyCap {
		t.Fatalf("the window holds %d events, above the %d cap", len(m.history), historyCap)
	}
	if len(m.history) == 0 {
		t.Fatal("the trim emptied the window")
	}

	// The first retained tick must be held *completely*. Every tick has exactly
	// groupSize events, so a partial group is immediately visible.
	first := m.history[0].Tick
	kept := 0
	for _, e := range m.history {
		if e.Tick == first {
			kept++
		}
	}
	if kept != groupSize {
		t.Fatalf("the window starts mid-group: tick %d is held %d times, want all %d of its events",
			first, kept, groupSize)
	}

	// And the base must name that tick, so `Since` knows where the window
	// begins.
	if m.base != first {
		t.Fatalf("base = %d, want the oldest retained tick %d", m.base, first)
	}
}

// TestRetainedWindowNeverSplitsATransition drives a match well past the
// retention cap and asserts that every tick the window still holds is held
// *completely*.
//
// The symptom this prevents is not theoretical. A client whose cursor sits one
// tick below the oldest retained tick passes the window check — `cursor+1 < base`
// is false when they are equal — and is served the tail of a split transition.
// It then applies half of a transition: a rent payment without the roll that
// caused it, or a turn advance without the purchase that preceded it.
func TestRetainedWindowNeverSplitsATransition(t *testing.T) {
	for _, seed := range fixedSeeds {
		t.Run(seedLabel(seed), func(t *testing.T) {
			f := newFixtureWithSeed(t, seed)
			f.playing(t)

			results := driveMatch(t, f, 400)

			// The window trims *to* the cap, so its length stays at the cap; what
			// proves the trim ran is that the base has moved off the genesis tick.
			if f.m.base == 0 {
				t.Fatalf("history still starts at the genesis tick after %d events: the trim never ran", len(f.m.history))
			}

			produced := tickEventCounts(results)
			if len(produced) == 0 {
				t.Fatal("no events were produced, so the assertion below would be vacuous")
			}
			retained := retainedCounts(f.m)

			checked := 0
			for tick, kept := range retained {
				total, known := produced[tick]
				if !known {
					continue
				}
				checked++
				if kept != total {
					t.Fatalf(
						"tick %d is split: the window retains %d of its %d events. "+
							"A client resuming from tick %d would be served a partial transition",
						tick, kept, total, tick-1,
					)
				}
			}
			if checked == 0 {
				t.Fatal("no retained tick could be compared against a produced tick: the assertion was vacuous")
			}
		})
	}
}

// seedLabel names a seed for a subtest.
func seedLabel(s game.Seed) string {
	return fmt.Sprintf("seed%02x", s[0])
}

// TestRetainedWindowServesWholeTransitionsToAResumingClient is the client-facing
// statement of the same invariant: a client that resumes from just below the
// oldest retained tick must receive whole transitions, never fragments.
func TestRetainedWindowServesWholeTransitionsToAResumingClient(t *testing.T) {
	for _, seed := range fixedSeeds {
		t.Run(seedLabel(seed), func(t *testing.T) {
			f := newFixtureWithSeed(t, seed)
			f.playing(t)

			results := driveMatch(t, f, 400)
			if f.m.base == 0 {
				t.Fatal("history still starts at the genesis tick: the trim never ran")
			}

			produced := tickEventCounts(results)
			oldest := f.m.history[0].Tick
			if oldest == 0 {
				t.Fatal("expected a non-zero oldest retained tick")
			}

			// A client sitting one tick below the oldest retained tick is inside the
			// window as far as the runtime is concerned, and must be served complete
			// transitions.
			events, ok := f.m.Since(oldest - 1)
			if !ok {
				t.Fatalf("Since(%d) reported the cursor as outside the retained window, so a "+
					"client would be told to resync instead of being served the tail of a transition", oldest-1)
			}
			seen := map[uint64]int{}
			for _, e := range events {
				seen[e.Tick]++
			}
			checked := 0
			for tick, kept := range seen {
				total, known := produced[tick]
				if !known {
					continue
				}
				checked++
				if kept != total {
					t.Fatalf("catch-up from tick %d delivered %d of tick %d's %d events: a partial transition",
						oldest-1, kept, tick, total)
				}
			}
			if checked == 0 {
				t.Fatal("no delivered tick could be compared: the assertion was vacuous")
			}
		})
	}
}
