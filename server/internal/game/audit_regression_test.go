package game

import (
	"strings"
	"testing"
)

// Regression tests for the v0.2.1 audit. Every test here reproduces a defect
// that was confirmed against the v0.2.0 tree; the comment on each names the
// behaviour it protects. Engine state is arranged white-box where a scenario
// needs it, but every mutation under test goes through Engine.Apply, and every
// assertion reads the resulting state or event stream.

var auditRoot = Seed{0x0A}

// --- ENG-1: leaving mid-match must return holdings to the bank -------------

// A player who leaves while owning property must not leave it behind. Before
// the fix, `leave` was the only elimination path that skipped the holdings
// sweep, so the spaces stayed owned by an eliminated player: permanently
// unpurchasable (buy and decline both reject an owned space) and permanently
// rent-free (a payer skips an inactive owner), with no event recording it.
func TestLeaveDuringPlayReleasesHoldings(t *testing.T) {
	e, state := testMatchEngine(t, DefaultConfig(), auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit", "Cid")

	// Give the first player a property directly, then leave through the public
	// action. White-box setup only arranges the scenario.
	victim := ids[0]
	prop := -1
	for i, sp := range state.Board.Spaces {
		if sp.Kind == SpaceProperty && !sp.Owned {
			prop = i
			break
		}
	}
	if prop < 0 {
		t.Fatal("test board has no unowned property")
	}
	state.Board.Spaces[prop].Owned = true
	state.Board.Spaces[prop].Owner = victim

	next, events := mustApply(t, e, state, victim, PlayerLeaveAction{})

	if next.Board.Spaces[prop].Owned {
		t.Fatalf("space %q is still owned after the owner left", next.Board.Spaces[prop].ID)
	}
	if next.Board.Spaces[prop].Owner != "" {
		t.Fatalf("space %q still names owner %q after the owner left",
			next.Board.Spaces[prop].ID, next.Board.Spaces[prop].Owner)
	}
	// The change must be reconstructible from the event log.
	var named bool
	for _, ev := range events {
		br, ok := ev.(*PlayerBankruptEvent)
		if !ok {
			continue
		}
		for _, id := range br.ReleasedSpaces {
			if id == next.Board.Spaces[prop].ID {
				named = true
			}
		}
	}
	if !named {
		t.Fatalf("no event names the released space %q; the log cannot explain the state",
			next.Board.Spaces[prop].ID)
	}
	// And the structural invariant must hold for every remaining player.
	assertNoActiveSpaceIsOrphaned(t, next)
}

// The space a departed player held must be purchasable again. Before the fix
// it stayed Owned, so a landing there produced no buy decision at all.
func TestSpaceHeldByLeaverIsPurchasableAgain(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit", "Cid")

	victim, prop := ids[0], -1
	for i, sp := range state.Board.Spaces {
		if sp.Kind == SpaceProperty && !sp.Owned {
			prop = i
			break
		}
	}
	if prop < 0 {
		t.Fatal("test board has no unowned property")
	}
	state = state.Clone()
	state.Board.Spaces[prop].Owned = true
	state.Board.Spaces[prop].Owner = victim

	state, _ = mustApply(t, e, state, victim, PlayerLeaveAction{})

	if state.Board.Spaces[prop].Owned {
		t.Fatalf("space %d is still owned by the departed player", prop)
	}

	// A different player landing there must be offered the buy decision.
	landed := state.Clone()
	other := ids[1]
	if p := landed.PlayerByID(other); p != nil {
		p.Money = cfg.StartingMoney
	}
	outcome, err := (BoardRuleset{}).resolveLanding(landed, &cfg, other, prop)
	if err != nil {
		t.Fatalf("resolveLanding: %v", err)
	}
	if !outcome.buyDecision {
		t.Fatal("a space released by a leaver must offer the buy decision again")
	}
}

// --- ENG-7: bankruptcy records the spaces it released ---------------------

// Reverting N properties to the bank with no event naming them makes the event
// log unable to reconstruct the post-state: a client or auditor could see that
// *something* was released, never which spaces.
func TestBankruptcyEventNamesReleasedSpaces(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	// Ace holds three properties; Bit holds the last space on the board. Ace is
	// aimed at Bit's property with no cash, so the mandatory rent payment
	// bankrupts it.
	//
	// The target is the final space deliberately: aimRoll positions Ace just
	// short of the target, and a low-index target would force it across the
	// start, where the pass-go bonus would hand Ace the money it is supposed to
	// be short of.
	state = state.Clone()
	var propertyIdx []int
	for i, sp := range state.Board.Spaces {
		if sp.Kind == SpaceProperty {
			propertyIdx = append(propertyIdx, i)
		}
	}
	if len(propertyIdx) < 5 {
		t.Fatalf("test board needs at least 5 properties, has %d", len(propertyIdx))
	}
	target := propertyIdx[len(propertyIdx)-1] // last property, highest index
	held := propertyIdx[:3]
	for _, i := range held {
		state.Board.Spaces[i].Owned = true
		state.Board.Spaces[i].Owner = ids[0]
	}
	state.Board.Spaces[target].Owned = true
	state.Board.Spaces[target].Owner = ids[1]

	ace := state.PlayerByID(ids[0])
	ace.Money = 0
	state.Turn.CurrentSeat = ace.Seat

	before := ace.Money
	after, events := aimRoll(t, e, auditRoot, state, ids[0], target)

	if before != 0 {
		t.Fatalf("scenario setup wrong: Ace started with %d", before)
	}
	var got []string
	var sawBankrupt bool
	for _, ev := range events {
		if br, ok := ev.(*PlayerBankruptEvent); ok {
			sawBankrupt = true
			got = br.ReleasedSpaces
		}
	}
	if !sawBankrupt {
		t.Fatalf("expected a bankruptcy on rent, got events %v", eventTypeNames(events))
	}
	if len(got) != len(held) {
		t.Fatalf("event released %v, want exactly the %d spaces held (%v)", got, len(held), held)
	}
	for _, i := range held {
		want := state.Board.Spaces[i].ID
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("released space %q missing from the event: %v", want, got)
		}
	}
	// The structural invariant must hold on the resulting state.
	assertNoActiveSpaceIsOrphaned(t, after)
	// And the released spaces must actually be unowned and purchasable again.
	for _, i := range held {
		if after.Board.Spaces[i].Owned {
			t.Fatalf("space %q is still owned after the bankruptcy", after.Board.Spaces[i].ID)
		}
	}
}

func eventTypeNames(events []Event) []string {
	names := make([]string, 0, len(events))
	for _, ev := range events {
		names = append(names, string(ev.Type()))
	}
	return names
}

// --- ENG-2: the start bonus is bounded -------------------------------------

// Money is int64 and the credit is `bonus * passes`, so an unbounded bonus could
// overflow a balance negative on a single roll. Every other money field already
// had a ceiling; this one did not.
func TestPassingGoBonusIsBounded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropertyRules.PassingGoBonus = MaxPassingGoBonus + 1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("an out-of-range passingGoBonus must be rejected")
	}
	if !strings.Contains(err.Error(), "passingGoBonus") {
		t.Fatalf("error should name the field, got %v", err)
	}
}

func TestPassingGoBonusRejectsNegative(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropertyRules.PassingGoBonus = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative passingGoBonus must be rejected")
	}
}

// The ceiling must hold on the raw-JSON tier too, not only after decoding.
func TestPassingGoBonusBoundedInRawJSON(t *testing.T) {
	raw := []byte(`{"version":1,
		"playerCount":{"min":2,"max":6},"startingMoney":1500,
		"trading":false,"auctions":false,"carnival":false,"sports":false,
		"gambling":{"enabled":false,"poker":false,"blackjack":false,"casino":false},
		"cards":false,"randomEvents":{"enabled":false,"intervalTicks":0},
		"victory":{"type":"last_standing"},
		"board":{"spaces":[
			{"id":"go","kind":"go","name":"Start"},
			{"id":"p1","kind":"property","name":"One","group":"G","price":100,"rent":10,"amount":0},
			{"id":"t1","kind":"tax","name":"Toll","group":"","price":0,"rent":0,"amount":50}
		]},
		"propertyRules":{"passingGoBonus":9007199254740992,"doublesExtraRoll":true,"maxDoublesStreak":3}}`)
	if _, err := ParseGameConfig(raw); err == nil {
		t.Fatal("a raw-JSON passingGoBonus beyond the ceiling must be rejected")
	}
}

// --- ENG-3: an out-of-range position is an error, not a panic --------------

// `roll` indexes Board.Spaces with the actor's position and requireTurn did not
// check it, so a state with an out-of-range position panicked inside the
// transition — on the goroutine holding the match lock, which wedged the match
// permanently.
func TestRollWithOutOfRangePositionReturnsError(t *testing.T) {
	e, state := testMatchEngine(t, DefaultConfig(), auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	state = state.Clone()
	for i := range state.Players {
		if state.Players[i].ID == ids[0] {
			state.Players[i].Position = len(state.Board.Spaces) + 5
		}
	}
	state.Turn.CurrentSeat = state.Players[0].Seat

	// Must not panic.
	_, _, err := e.Apply(state, ids[0], RollDiceAction{})
	if err == nil {
		t.Fatal("a roll from an out-of-bounds position must be refused, not panic")
	}
}

func TestUnmarshalRejectsOutOfRangePosition(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	state = state.Clone()
	for i := range state.Players {
		if state.Players[i].ID == ids[0] {
			state.Players[i].Position = 9999
		}
	}
	blob, err := MarshalGameState(state)
	if err != nil {
		t.Fatal(err)
	}
	// The corrupt state must be refused at load time, not discovered later by
	// panicking on the next roll.
	if _, err := UnmarshalGameState(blob); err == nil {
		t.Fatal("loading a state with an out-of-range position must fail")
	}
}

// --- ENG-4: a missing root seed is an error, not a panic --------------------

// RootSeedHex is optional on the wire and was never validated on load, so a
// snapshot written by a build that predates the field unmarshalled cleanly and
// then panicked on the next join or start.
func TestUnmarshalRejectsMissingRootSeed(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	state = state.Clone()
	state.RootSeedHex = ""
	blob, err := MarshalGameState(state)
	if err != nil {
		t.Fatal(err)
	}
	// MarshalGameState omits the empty field, exactly as an older build would.
	loaded, err := UnmarshalGameState(blob)
	if err == nil {
		// If it loads, applying must still produce an error rather than a panic.
		_, _, aerr := e.Apply(loaded, ids[0], PlayerJoinAction{DisplayName: "Zed"})
		if aerr == nil {
			t.Fatal("joining with no usable root seed must fail")
		}
	}
}

func TestJoinWithMissingRootSeedReturnsError(t *testing.T) {
	e, state := testMatchEngine(t, DefaultConfig(), auditRoot)
	state = state.Clone()
	state.RootSeedHex = "not-a-seed"

	// Must return an error rather than panicking.
	_, _, err := e.Apply(state, "", PlayerJoinAction{DisplayName: "Ace"})
	if err == nil {
		t.Fatal("a corrupt root seed must be reported, not panic")
	}
}

func TestStartWithMissingRootSeedReturnsError(t *testing.T) {
	e, state := testMatchEngine(t, DefaultConfig(), auditRoot)
	ids, state := joinAndReady(t, e, state, "Ace", "Bit")
	_ = ids
	state = state.Clone()
	state.RootSeedHex = ""

	if _, _, err := e.Apply(state, state.Host().ID, GameStartAction{}); err == nil {
		t.Fatal("starting with a corrupt root seed must be reported, not panic")
	}
}

// --- ENG-5: wealth ties are broken by seat, not slice position -------------

// Seats are assigned as the lowest free seat, so after a lobby departure and
// rejoin the seat order and the slice order diverge. Scanning the slice awarded
// a tied wealth victory to the wrong player, contradicting the documented rule.
func TestWealthTieIsBrokenByLowestSeat(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)

	// Build the divergence in the lobby: a seat is vacated, then the next
	// joiner takes the lowest free seat, which puts them later in the slice
	// than the players who joined first.
	ids, state := joinAndReady(t, e, state, "Ace", "Bit", "Cid")
	state, _ = mustApply(t, e, state, ids[0], PlayerLeaveAction{})
	_, events := mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "Dee"})
	newcomer := events[0].(*PlayerJoinedEvent).PlayerID

	// Confirm the two orders actually differ, or the test proves nothing.
	lowestSeat := state.Players[0]
	for _, p := range state.Players {
		if p.Seat < lowestSeat.Seat {
			lowestSeat = p
		}
	}
	if lowestSeat.ID == newcomer {
		t.Skip("seat order and slice order coincide; nothing to distinguish")
	}
	if state.Players[0].ID == lowestSeat.ID {
		t.Skip("lobby orders coincide; nothing to distinguish")
	}

	// Equal net worth for everyone still in the lobby, then ask which player the
	// wealth rule would pick. The documented rule is "ties to the lowest seat".
	equalized := state.Clone()
	for i := range equalized.Players {
		equalized.Players[i].Money = cfg.StartingMoney
	}
	winner := richestActive(equalized)
	if winner == nil {
		t.Fatal("expected an active player")
	}
	if winner.ID != lowestSeat.ID {
		t.Fatalf("tie went to seat %d (%s) but the lowest seat is %d (%s)",
			winner.Seat, winner.Name, lowestSeat.Seat, lowestSeat.Name)
	}
}

// --- ENG-6: Clone deep-copies the winner pointer ---------------------------

// WinnerID is a pointer, and GameEndedEvent captures the same allocation, so a
// struct copy left state and a persisted historical event sharing one pointer.
func TestCloneDeepCopiesWinnerID(t *testing.T) {
	cfg := DefaultConfig()
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	// End the match so a winner is recorded.
	state, _ = mustApply(t, e, state, ids[1], PlayerLeaveAction{})
	if state.Phase != PhaseEnded {
		t.Fatalf("expected the match to end, got %q", state.Phase)
	}
	if state.WinnerID == nil {
		t.Skip("no winner recorded for this path")
	}

	a := state.Clone()
	b := state.Clone()
	if a.WinnerID == b.WinnerID {
		t.Fatal("two clones share the same winner pointer")
	}
	// Writing through one must not be visible in the other.
	replacement := PlayerID("pl_someone_else")
	*a.WinnerID = replacement
	if *b.WinnerID == replacement {
		t.Fatal("writing through one clone's winner changed the other")
	}
	if *state.WinnerID == replacement {
		t.Fatal("writing through a clone's winner changed the original")
	}
}

// --- ENG-8: config and phase edges -----------------------------------------

// A streak of 1 made doublesExtraRoll a silent no-op: the first double resolved
// the roll and passed the turn exactly as if the rule were disabled.
func TestDoublesExtraRollRequiresAUsableStreak(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropertyRules.DoublesExtraRoll = true
	cfg.PropertyRules.MaxDoublesStreak = 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("maxDoublesStreak 1 with doublesExtraRoll must be rejected, not silently ignored")
	}
}

func TestDoublesExtraRollMayBeDisabledWithAStreakOfOne(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropertyRules.DoublesExtraRoll = false
	cfg.PropertyRules.MaxDoublesStreak = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("with the rule disabled the streak is irrelevant and must be accepted: %v", err)
	}
}

func TestDoublesStreakIsBounded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropertyRules.MaxDoublesStreak = MaxDoublesStreakCeiling + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("an unbounded doubles streak must be rejected")
	}
}

// Leave was the only action whose phase guard named Ended rather than every
// terminal phase, so it could mutate an interrupted match — one that is
// terminal and never resumed.
func TestLeaveRejectsInterruptedMatch(t *testing.T) {
	e, state := testMatchEngine(t, DefaultConfig(), auditRoot)
	ids, state := startBoardMatch(t, e, state, "Ace", "Bit")

	interrupted := state.Clone()
	interrupted.Phase = PhaseInterrupted
	before := interrupted.Clone()

	_, _, err := e.Apply(interrupted, ids[0], PlayerLeaveAction{})
	if err != ErrOutOfPhase {
		t.Fatalf("err = %v, want ErrOutOfPhase for an interrupted match", err)
	}
	if interrupted.Players[0].Status != before.Players[0].Status {
		t.Fatal("a rejected leave mutated the roster")
	}
	if interrupted.Tick != before.Tick {
		t.Fatal("a rejected leave advanced the tick")
	}
}

// Starting with too few players reported "match is full", which is the first
// thing every new player does and produced a dead end with no way to understand
// the problem.
func TestStartWithTooFewPlayersIsNotReportedAsFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlayerCount = PlayerCountRange{Min: 4, Max: 8}
	e, state := testMatchEngine(t, cfg, auditRoot)
	ids, state := joinAndReady(t, e, state, "Ace", "Bit")

	_, _, err := e.Apply(state, ids[0], GameStartAction{})
	if err == nil {
		t.Fatal("starting a 4-player match with 2 players must fail")
	}
	if err == ErrGameFull {
		t.Fatal("too few players must not be reported as a full match")
	}
	if err != ErrNotEnoughPlayers {
		t.Fatalf("err = %v, want ErrNotEnoughPlayers", err)
	}
}

// The too-few and too-many cases must stay distinguishable: collapsing them
// again would tell a host with an empty table that the table is full.
// --- ENG-10: validation errors are deterministically ordered ---------------

// ValidationError.Issues was built while ranging Go maps, so two identical
// malformed requests produced different error text.
func TestValidationErrorOrderIsDeterministic(t *testing.T) {
	raw := []byte(`{"version":1,
		"playerCount":{"min":2,"max":6},"startingMoney":1500,
		"trading":false,"auctions":false,"carnival":false,"sports":false,
		"gambling":{"enabled":false,"poker":false,"blackjack":false,"casino":false},
		"cards":false,"randomEvents":{"enabled":false,"intervalTicks":0},
		"victory":{"type":"last_standing"},
		"board":{"spaces":[
			{"id":"go","kind":"go","name":"Start"},
			{"id":"p1","kind":"property","name":"One","group":"G","price":100,"rent":10,"amount":0}
		]},
		"propertyRules":{"passingGoBonus":200,"doublesExtraRoll":true,"maxDoublesStreak":3},
		"zeta":1,"alpha":2,"omega":3,"beta":4,"gamma":5,"delta":6,"epsilon":7,
		"theta":8,"iota":9,"kappa":10,"lambda":11,"mu":12,"nu":13,"xi":14,
		"omicron":15,"pi":16,"rho":17,"sigma":18,"tau":19,"upsilon":20}`)

	_, first := ParseGameConfig(raw)
	if first == nil {
		t.Fatal("expected the unknown fields to be rejected")
	}
	firstText := first.Error()
	for i := 0; i < 200; i++ {
		_, err := ParseGameConfig(raw)
		if err == nil {
			t.Fatal("expected consistent rejection")
		}
		if err.Error() != firstText {
			t.Fatalf("validation error text is not deterministic:\n%q\nvs\n%q", firstText, err.Error())
		}
	}
}

// --- helpers ---------------------------------------------------------------

// assertNoActiveSpaceIsOrphaned checks the invariant that no space is owned by
// an eliminated player. Before the fix, leaving mid-match broke it.
func assertNoActiveSpaceIsOrphaned(t *testing.T, state *GameState) {
	t.Helper()
	for _, sp := range state.Board.Spaces {
		if !sp.Owned {
			continue
		}
		owner := state.PlayerByID(sp.Owner)
		if owner == nil {
			t.Fatalf("space %q is owned by unknown player %q", sp.ID, sp.Owner)
		}
		if !owner.Active() {
			t.Fatalf("space %q is owned by eliminated player %q", sp.ID, owner.Name)
		}
	}
}
