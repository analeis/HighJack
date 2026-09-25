package game

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Board tests use MatchRuleset (lifecycle + board) with a fixed root seed.
// Dice are never hardcoded: diceFor predicts the exact dice the engine will
// produce for a state tick using the same stream construction as Apply, so
// aimRoll can land deterministically while every transition stays public.
// White-box state setup (positions, money) arranges scenarios only; every
// mutation under test goes through Engine.Apply.
func testMatchEngine(t *testing.T, cfg GameConfig, root Seed) (*Engine, *GameState) {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	e, err := NewEngine(&cfg, root, MatchRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	return e, e.NewState("game_board_test")
}

func startBoardMatch(t *testing.T, e *Engine, state *GameState, names ...string) ([]PlayerID, *GameState) {
	t.Helper()
	ids, state := joinAndReady(t, e, state, names...)
	host := state.Host()
	if host == nil {
		t.Fatal("no host after joins")
	}
	var events []Event
	state, events = mustApply(t, e, state, host.ID, GameStartAction{})
	if len(events) != 1 {
		t.Fatalf("start must emit exactly game_started, got %+v", events)
	}
	return ids, state
}

// diceFor predicts the dice Engine.Apply will roll for a transition applied
// at the given tick. Mirrors the per-tick stream construction in Apply.
func diceFor(root Seed, tick uint64) (int, int) {
	rng := NewSeededRng(Split(root, fmt.Sprintf("tick:%d", tick)))
	return rng.IntN(6) + 1, rng.IntN(6) + 1
}

// aimRoll arranges actor's position so the upcoming roll lands exactly on
// targetIdx, then applies the roll. Returns the resulting state and events.
func aimRoll(t *testing.T, e *Engine, root Seed, state *GameState, actor PlayerID, targetIdx int) (*GameState, []Event) {
	t.Helper()
	n := len(state.Board.Spaces)
	d1, d2 := diceFor(root, state.Tick)
	pos := ((targetIdx-(d1+d2))%n + n) % n
	if p := state.PlayerByID(actor); p == nil {
		t.Fatalf("aimRoll: unknown actor %s", actor)
	} else {
		p.Position = pos
	}
	return mustApply(t, e, state, actor, RollDiceAction{})
}

// finishTurn plays actor's turn to completion: rolls, declines every buy
// decision, ends the turn when required. Returns when the seat passes or
// the match ends. The iteration cap guards against logic errors, not
// randomness: every path below strictly progresses the turn.
func finishTurn(t *testing.T, e *Engine, state *GameState, actor PlayerID) *GameState {
	t.Helper()
	p := state.PlayerByID(actor)
	if p == nil {
		t.Fatalf("finishTurn: unknown actor %s", actor)
	}
	seat := p.Seat
	for i := 0; i < 50; i++ {
		if state.Phase != PhasePlaying || state.Turn.CurrentSeat != seat {
			return state
		}
		switch state.Turn.Phase {
		case TurnAwaitRoll:
			state, _ = mustApply(t, e, state, actor, RollDiceAction{})
		case TurnAwaitBuyDecision:
			state, _ = mustApply(t, e, state, actor, DeclineBuyAction{})
		case TurnOver:
			state, _ = mustApply(t, e, state, actor, EndTurnAction{})
		default:
			t.Fatalf("unknown turn phase %q", state.Turn.Phase)
		}
	}
	t.Fatalf("turn did not complete within 50 actions: %+v", state.Turn)
	return state
}

func rollEvent(t *testing.T, events []Event) *DiceRolledEvent {
	t.Helper()
	for _, ev := range events {
		if d, ok := ev.(*DiceRolledEvent); ok {
			return d
		}
	}
	t.Fatalf("no dice_rolled event in %+v", events)
	return nil
}

func findEvent[T Event](t *testing.T, events []Event) T {
	t.Helper()
	for _, ev := range events {
		if e, ok := ev.(T); ok {
			return e
		}
	}
	var zero T
	t.Fatalf("event %T not found in %+v", zero, events)
	return zero
}

func currentSeatPlayer(t *testing.T, state *GameState) *Player {
	t.Helper()
	for i := range state.Players {
		if state.Players[i].Seat == state.Turn.CurrentSeat {
			return &state.Players[i]
		}
	}
	t.Fatalf("no player holds current seat %d", state.Turn.CurrentSeat)
	return nil
}

func TestStartInitializesBoardAndTurn(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	if len(state.Board.Spaces) != 24 {
		t.Fatalf("default board must have 24 spaces, got %d", len(state.Board.Spaces))
	}
	for _, s := range state.Board.Spaces {
		if s.Owned || s.Owner != "" || s.Level != 0 {
			t.Fatalf("board must start unowned: %+v", s)
		}
	}
	if state.Turn != (TurnState{CurrentSeat: 0, Phase: TurnAwaitRoll, Round: 1}) {
		t.Fatalf("unexpected initial turn: %+v", state.Turn)
	}
	for _, id := range ids {
		if p := state.PlayerByID(id); p.Position != 0 {
			t.Fatalf("players must start on space 0: %+v", p)
		}
	}
}

func TestRollMovesAndAdvancesTurn(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	d1, d2 := diceFor(root, state.Tick)
	state, events := mustApply(t, e, state, ids[0], RollDiceAction{})
	d := rollEvent(t, events)
	if d.Die1 != d1 || d.Die2 != d2 {
		t.Fatalf("dice mismatch: got %d,%d want %d,%d", d.Die1, d.Die2, d1, d2)
	}
	if d.Die1 < 1 || d.Die1 > 6 || d.Die2 < 1 || d.Die2 > 6 {
		t.Fatalf("dice out of range: %+v", d)
	}
	want := (d1 + d2) % 24
	if p := state.PlayerByID(ids[0]); p.Position != want {
		t.Fatalf("position = %d, want %d", p.Position, want)
	}
	if d.ToSpace != state.Board.Spaces[want].ID {
		t.Fatalf("dice event destination %q != space %q", d.ToSpace, state.Board.Spaces[want].ID)
	}
}

func TestRollGating(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// Wrong player.
	if _, _, err := e.Apply(state, ids[1], RollDiceAction{}); err != ErrNotYourTurn {
		t.Fatalf("expected ErrNotYourTurn, got %v", err)
	}
	// Unknown actor.
	if _, _, err := e.Apply(state, "pl_NOBODY", RollDiceAction{}); err != ErrPlayerNotInGame {
		t.Fatalf("expected ErrPlayerNotInGame, got %v", err)
	}
	// Roll in lobby.
	_, lobby := testMatchEngine(t, DefaultConfig(), root)
	if _, _, err := e.Apply(lobby, ids[0], RollDiceAction{}); err != ErrOutOfPhase {
		t.Fatalf("expected ErrOutOfPhase in lobby, got %v", err)
	}
	// Buy without a decision pending.
	if _, _, err := e.Apply(state, ids[0], BuyPropertyAction{}); err != ErrOutOfPhase {
		t.Fatalf("expected ErrOutOfPhase for early buy, got %v", err)
	}
	// End turn before anything resolved.
	if _, _, err := e.Apply(state, ids[0], EndTurnAction{}); err != ErrOutOfPhase {
		t.Fatalf("expected ErrOutOfPhase for early end_turn, got %v", err)
	}
}

func TestBuyHappyPath(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// Land A on a1 (index 1, price 100) deterministically.
	state, events := aimRoll(t, e, root, state, ids[0], 1)
	d := rollEvent(t, events)
	if d.ToSpace != "a1" {
		t.Fatalf("aimed at a1, landed %q", d.ToSpace)
	}
	if state.Turn.Phase != TurnAwaitBuyDecision {
		t.Fatalf("affordable unowned landing must await decision, phase=%q", state.Turn.Phase)
	}

	before := state.PlayerByID(ids[0]).Money
	state, events = mustApply(t, e, state, ids[0], BuyPropertyAction{})
	bought := findEvent[*PropertyBoughtEvent](t, events)
	if bought.SpaceID != "a1" || bought.Price != 100 {
		t.Fatalf("unexpected purchase event: %+v", bought)
	}
	p := state.PlayerByID(ids[0])
	if p.Money != before-100 {
		t.Fatalf("money = %d, want %d", p.Money, before-100)
	}
	space := state.Board.Spaces[1]
	if !space.Owned || space.Owner != ids[0] {
		t.Fatalf("ownership not recorded: %+v", space)
	}
	// Doubles grant an extra roll, otherwise the decision ends the turn.
	if d.Die1 == d.Die2 {
		if state.Turn.Phase != TurnAwaitRoll {
			t.Fatalf("doubles buy must return to await_roll, got %q", state.Turn.Phase)
		}
	} else if state.Turn.Phase != TurnOver {
		t.Fatalf("buy without doubles must end decision at turn_over, got %q", state.Turn.Phase)
	}
}

func TestDeclineResolvesDecision(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	state, events := aimRoll(t, e, root, state, ids[0], 1)
	d := rollEvent(t, events)
	if state.Turn.Phase != TurnAwaitBuyDecision {
		t.Fatalf("expected buy decision, got %q", state.Turn.Phase)
	}
	before := mustJSON(t, state.PlayerByID(ids[0]))
	state, events = mustApply(t, e, state, ids[0], DeclineBuyAction{})
	declined := findEvent[*BuyDeclinedEvent](t, events)
	if declined.SpaceID != "a1" {
		t.Fatalf("unexpected decline event: %+v", declined)
	}
	if mustJSON(t, state.PlayerByID(ids[0])) != before {
		t.Fatal("decline must not move money")
	}
	if state.Board.Spaces[1].Owned {
		t.Fatal("declined property must stay unowned")
	}
	if d.Die1 == d.Die2 {
		if state.Turn.Phase != TurnAwaitRoll {
			t.Fatalf("doubles decline must return to await_roll, got %q", state.Turn.Phase)
		}
	} else if state.Turn.Phase != TurnOver {
		t.Fatalf("decline must resolve to turn_over, got %q", state.Turn.Phase)
	}
}

func TestRentTransferExact(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// A buys b1 (index 8, price 180, rent 18), then A's turn completes.
	state, _ = aimRoll(t, e, root, state, ids[0], 8)
	state, _ = mustApply(t, e, state, ids[0], BuyPropertyAction{})
	state = finishTurn(t, e, state, ids[0])

	// B lands on b1 and pays exactly 18 to A.
	aBefore, bBefore := state.PlayerByID(ids[0]).Money, state.PlayerByID(ids[1]).Money
	state, events := aimRoll(t, e, root, state, ids[1], 8)
	rent := findEvent[*RentPaidEvent](t, events)
	if rent.Amount != 18 || rent.FromPlayerID != ids[1] || rent.ToPlayerID != ids[0] {
		t.Fatalf("unexpected rent event: %+v", rent)
	}
	if got := state.PlayerByID(ids[0]).Money; got != aBefore+18 {
		t.Fatalf("owner money = %d, want %d", got, aBefore+18)
	}
	if got := state.PlayerByID(ids[1]).Money; got != bBefore-18 {
		t.Fatalf("payer money = %d, want %d", got, bBefore-18)
	}
}

func TestWrapAroundAwardsPassGo(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// Park A on space 23: any roll wraps (min total 2).
	state.PlayerByID(ids[0]).Position = 23
	d1, d2 := diceFor(root, state.Tick)
	before := state.PlayerByID(ids[0]).Money
	state, events := mustApply(t, e, state, ids[0], RollDiceAction{})
	d := rollEvent(t, events)
	if !d.PassedGo {
		t.Fatal("wrapping roll must flag passedGo")
	}
	if want := (23 + d1 + d2) % 24; state.PlayerByID(ids[0]).Position != want {
		t.Fatalf("position = %d, want %d", state.PlayerByID(ids[0]).Position, want)
	}
	var bonus *BankTransferEvent
	for _, ev := range events {
		if b, ok := ev.(*BankTransferEvent); ok && b.Direction == "to_player" {
			bonus = b
		}
	}
	if bonus == nil || bonus.Amount != 200 {
		t.Fatalf("expected 200 pass-go bonus event, got %+v", events)
	}
	paid := Money(0)
	for _, ev := range events {
		switch e := ev.(type) {
		case *RentPaidEvent:
			paid += e.Amount
		case *BankTransferEvent:
			if e.Direction == "to_bank" {
				paid += e.Amount
			}
		}
	}
	if got := state.PlayerByID(ids[0]).Money; got != before+200-paid {
		t.Fatalf("money = %d, want %d", got, before+200-paid)
	}
}

func TestLandingOnGoCollectsOnce(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	state, events := aimRoll(t, e, root, state, ids[0], 0)
	d := rollEvent(t, events)
	if d.ToSpace != "go" {
		t.Fatalf("aimed at go, landed %q", d.ToSpace)
	}
	if !d.PassedGo {
		t.Fatal("landing exactly on go counts as a crossing")
	}
	count, total := 0, Money(0)
	for _, ev := range events {
		if b, ok := ev.(*BankTransferEvent); ok && b.Direction == "to_player" {
			count++
			total += b.Amount
			if b.Reason != "land_go" {
				t.Fatalf("landing on go must use land_go reason, got %q", b.Reason)
			}
		}
	}
	if count != 1 || total != 200 {
		t.Fatalf("landing on go must pay exactly one 200 bonus, got %+v", events)
	}
}

func TestTaxPaymentToBank(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	before := state.PlayerByID(ids[0]).Money
	state, events := aimRoll(t, e, root, state, ids[0], 4) // t1, tax 75
	// The route may cross go (aiming wraps the ring); fold all bank flows.
	foundTax := false
	delta := Money(0)
	for _, ev := range events {
		if b, ok := ev.(*BankTransferEvent); ok {
			switch {
			case b.Direction == "to_bank" && b.Reason == "tax" && b.Amount == 75:
				foundTax = true
				delta -= b.Amount
			case b.Direction == "to_player":
				delta += b.Amount
			default:
				t.Fatalf("unexpected bank event: %+v", b)
			}
		}
	}
	if !foundTax {
		t.Fatalf("tax landing must emit a tax transfer: %+v", events)
	}
	if got := state.PlayerByID(ids[0]).Money; got != before+delta {
		t.Fatalf("money = %d, want %d", got, before+delta)
	}
}

func TestBankruptcyOnUnaffordableRent(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// A buys d4 (index 23, rent 40) and completes the turn; B is broke.
	// The owner's balance is captured after A's turn because extra rolls
	// may move it; bankruptcy itself must not touch it.
	state, _ = aimRoll(t, e, root, state, ids[0], 23)
	state, _ = mustApply(t, e, state, ids[0], BuyPropertyAction{})
	state = finishTurn(t, e, state, ids[0])
	aBefore := state.PlayerByID(ids[0]).Money
	state.PlayerByID(ids[1]).Money = 30

	state, events := aimRoll(t, e, root, state, ids[1], 23)
	bankrupt := findEvent[*PlayerBankruptEvent](t, events)
	if bankrupt.CreditorID != ids[0] || bankrupt.AmountOwed != 40 {
		t.Fatalf("unexpected bankruptcy event: %+v", bankrupt)
	}
	findEvent[*PlayerEliminatedEvent](t, events)
	if p := state.PlayerByID(ids[1]); p.Status != PlayerEliminated || p.Money < 0 {
		t.Fatalf("bankrupt player must be eliminated with non-negative money: %+v", p)
	}
	// No rent changes hands on a failed payment.
	for _, ev := range events {
		if _, ok := ev.(*RentPaidEvent); ok {
			t.Fatalf("failed rent must not emit rent_paid: %+v", events)
		}
	}
	if got := state.PlayerByID(ids[0]).Money; got != aBefore {
		t.Fatalf("owner money = %d, want %d", got, aBefore)
	}
	// With one survivor the match ends and they win.
	if state.Phase != PhaseEnded || state.WinnerID == nil || *state.WinnerID != ids[0] {
		t.Fatalf("sole survivor must win: %+v", state)
	}
	ended := findEvent[*GameEndedEvent](t, events)
	if ended.Reason != "last_standing" {
		t.Fatalf("reason = %q, want last_standing", ended.Reason)
	}
}

func TestBankruptcyRevertsHoldings(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B", "C")

	// B buys a1; C buys d4; B bankrupts on d4 with C still standing.
	state, _ = aimRoll(t, e, root, state, ids[0], 7)
	state = finishTurn(t, e, state, ids[0])
	state, _ = aimRoll(t, e, root, state, ids[1], 1)
	state, _ = mustApply(t, e, state, ids[1], BuyPropertyAction{})
	state = finishTurn(t, e, state, ids[1])
	state, _ = aimRoll(t, e, root, state, ids[2], 23)
	state, _ = mustApply(t, e, state, ids[2], BuyPropertyAction{})
	state = finishTurn(t, e, state, ids[2])
	// Rotation is A → B → C → A: complete A's turn to reach B again.
	state = finishTurn(t, e, state, ids[0])
	state.PlayerByID(ids[1]).Money = 10
	// B's turn again: land on d4.
	if cur := currentSeatPlayer(t, state); cur.ID != ids[1] {
		t.Fatalf("expected B's turn, current: %+v", state.Turn)
	}
	state, events := aimRoll(t, e, root, state, ids[1], 23)
	findEvent[*PlayerBankruptEvent](t, events)
	if s := state.Board.Spaces[1]; s.Owned {
		t.Fatalf("bankrupt holdings must revert to the bank: %+v", s)
	}
	if s := state.Board.Spaces[23]; !s.Owned || s.Owner != ids[2] {
		t.Fatalf("creditor holding must be untouched: %+v", s)
	}
	if state.Phase != PhasePlaying {
		t.Fatalf("two survivors must keep playing: %+v", state.Turn)
	}
	if cur := currentSeatPlayer(t, state); !cur.Active() {
		t.Fatalf("turn must rest on an active player: %+v", state.Turn)
	}
}

func TestDoublesGrantExtraRoll(t *testing.T) {
	root := findDoublesRoot(t)
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	state, events := mustApply(t, e, state, ids[0], RollDiceAction{})
	d := rollEvent(t, events)
	if d.Die1 != d.Die2 {
		t.Fatalf("doubles seed did not produce doubles: %+v", d)
	}
	if state.Turn.Phase == TurnAwaitBuyDecision {
		if !state.Turn.AwardExtraRoll || state.Turn.DoublesStreak != 1 {
			t.Fatalf("buy decision must remember the extra roll: %+v", state.Turn)
		}
		state, _ = mustApply(t, e, state, ids[0], DeclineBuyAction{})
	}
	if state.Turn.Phase != TurnAwaitRoll || state.Turn.CurrentSeat != state.PlayerByID(ids[0]).Seat {
		t.Fatalf("doubles must return to await_roll for the same player: %+v", state.Turn)
	}
	if state.Turn.DoublesStreak != 1 {
		t.Fatalf("streak must be 1, got %+v", state.Turn)
	}
}

// findDoublesRoot scans fixed seeds for one whose first roll is doubles.
func findDoublesRoot(t *testing.T) Seed {
	t.Helper()
	return findRootWith(t, func(d1, d2 int) bool { return d1 == d2 })
}

func TestEndTurnAdvancesSeatAndRound(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	state = finishTurn(t, e, state, ids[0])
	if state.Turn.CurrentSeat != 1 || state.Turn.Round != 1 || state.Turn.Phase != TurnAwaitRoll {
		t.Fatalf("after A's turn: %+v", state.Turn)
	}
	state = finishTurn(t, e, state, ids[1])
	if state.Turn.CurrentSeat != 0 || state.Turn.Round != 2 {
		t.Fatalf("wrap must advance to seat 0 round 2: %+v", state.Turn)
	}
}

func TestRoundLimitVictory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Victory = VictoryConfig{Type: VictoryRoundLimit, RoundLimit: 1}
	root := Seed{0x0B}
	e, state := testMatchEngine(t, cfg, root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	state = finishTurn(t, e, state, ids[0])
	if state.Phase != PhasePlaying {
		t.Fatalf("round 1 must continue: %+v", state.Turn)
	}
	state = finishTurn(t, e, state, ids[1])
	if state.Phase != PhaseEnded {
		t.Fatalf("round 2 > limit 1 must end the match: %+v", state.Turn)
	}
	if state.EndReason != "round_limit" || state.WinnerID == nil {
		t.Fatalf("unexpected end metadata: %+v", state)
	}
	var richest PlayerID
	var best Money = -1
	for _, id := range ids {
		if w := NetWorth(state, id); w > best {
			best, richest = w, id
		}
	}
	if *state.WinnerID != richest {
		t.Fatalf("winner %s != richest %s", *state.WinnerID, richest)
	}
}

func TestTargetWealthVictory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Victory = VictoryConfig{Type: VictoryTargetWealth, TargetWealth: 1600}
	// Non-doubles first roll: A's turn then completes with exactly one
	// roll, one optional decline, and one end-turn — no wrap possible from
	// space 0, so A cannot cross the target first.
	root := findRootWith(t, func(d1, d2 int) bool { return d1 != d2 })
	e, state := testMatchEngine(t, cfg, root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	// B sits below the target; A's first roll (no wrap possible from
	// space 0) cannot end anything.
	state.PlayerByID(ids[1]).Money = 1599
	state, _ = mustApply(t, e, state, ids[0], RollDiceAction{})
	if state.Phase != PhasePlaying {
		t.Fatal("A's roll alone must not end anything")
	}
	state = finishTurn(t, e, state, ids[0])
	// B crosses the target: no affordable landing can drop 5000 below it.
	state.PlayerByID(ids[1]).Money = 5000
	state, events := mustApply(t, e, state, ids[1], RollDiceAction{})
	ended := findEvent[*GameEndedEvent](t, events)
	if state.Phase != PhaseEnded || ended.Reason != "target_wealth" {
		t.Fatalf("crossing the target must end the match: %+v", events)
	}
	if state.WinnerID == nil || *state.WinnerID != ids[1] {
		t.Fatalf("B must win: %+v", state.WinnerID)
	}
}

// findRootWith scans fixed seeds for one whose first roll (tick 5)
// satisfies pred. The scan bounds are fixed, so results reproduce.
func findRootWith(t *testing.T, pred func(d1, d2 int) bool) Seed {
	t.Helper()
	for b := 0; b < 256; b++ {
		root := Seed{byte(b)}
		if d1, d2 := diceFor(root, 5); pred(d1, d2) {
			return root
		}
	}
	t.Fatal("no matching seed in scan range")
	return Seed{}
}

func TestInvalidActionsLeaveStateUntouched(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B")

	rejects := []struct {
		name   string
		actor  PlayerID
		action Action
	}{
		{"wrong player rolls", ids[1], RollDiceAction{}},
		{"buy with no decision", ids[0], BuyPropertyAction{}},
		{"end turn too early", ids[0], EndTurnAction{}},
		{"decline with no decision", ids[0], DeclineBuyAction{}},
		{"unknown actor", "pl_NOBODY", RollDiceAction{}},
	}
	for _, tc := range rejects {
		before := mustJSON(t, state)
		if _, _, err := e.Apply(state, tc.actor, tc.action); err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
		if after := mustJSON(t, state); after != before {
			t.Fatalf("%s: rejected action mutated state", tc.name)
		}
	}

	// Eliminated players cannot act.
	e2, s2 := testMatchEngine(t, DefaultConfig(), root)
	ids2, s2 := startBoardMatch(t, e2, s2, "A", "B", "C")
	s2, _ = mustApply(t, e2, s2, ids2[0], PlayerLeaveAction{})
	before := mustJSON(t, s2)
	if _, _, err := e2.Apply(s2, ids2[0], RollDiceAction{}); err != ErrNotPermitted {
		t.Fatalf("eliminated actor must be rejected, got %v", err)
	}
	if after := mustJSON(t, s2); after != before {
		t.Fatal("eliminated action mutated state")
	}
}

// TestBoardDeterminism reruns a scripted multi-turn match twice and
// requires byte-identical states and event logs.
func TestBoardDeterminism(t *testing.T) {
	run := func() (string, []string) {
		root := Seed{0x0B}
		e, state := testMatchEngine(t, DefaultConfig(), root)
		ids, state := startBoardMatch(t, e, state, "A", "B", "C")
		var log []string
		for round := 0; round < 4 && state.Phase == PhasePlaying; round++ {
			for _, id := range ids {
				if state.Phase != PhasePlaying {
					break
				}
				p := state.PlayerByID(id)
				if p == nil || !p.Active() || p.Seat != state.Turn.CurrentSeat {
					continue
				}
				state = finishTurn(t, e, state, id)
				// finishTurn consumes whole turns; re-derive the log from
				// per-action application instead for exactness.
				_ = log
			}
		}
		return mustJSON(t, state), log
	}
	_ = run
	// Precise per-action logging variant.
	runLogged := func() (string, []string) {
		root := Seed{0x0B}
		e, state := testMatchEngine(t, DefaultConfig(), root)
		ids, state := startBoardMatch(t, e, state, "A", "B", "C")
		var log []string
		apply := func(actor PlayerID, action Action) {
			var events []Event
			state, events = mustApply(t, e, state, actor, action)
			log = append(log, renderEvents(events)...)
		}
		for round := 0; round < 4 && state.Phase == PhasePlaying; round++ {
			for _, id := range ids {
				p := state.PlayerByID(id)
				if state.Phase != PhasePlaying || p == nil || !p.Active() || p.Seat != state.Turn.CurrentSeat {
					continue
				}
				apply(id, RollDiceAction{})
				if state.Phase != PhasePlaying {
					break
				}
				if state.Turn.Phase == TurnAwaitBuyDecision {
					apply(id, DeclineBuyAction{})
				}
				if state.Phase == PhasePlaying && state.Turn.Phase == TurnOver && state.Turn.CurrentSeat == p.Seat {
					apply(id, EndTurnAction{})
				}
			}
		}
		return mustJSON(t, state), log
	}
	a, logA := runLogged()
	b, logB := runLogged()
	if a != b {
		t.Fatalf("states diverged:\n%s\n---\n%s", a, b)
	}
	if len(logA) != len(logB) {
		t.Fatalf("log lengths diverged: %d vs %d", len(logA), len(logB))
	}
	for i := range logA {
		if logA[i] != logB[i] {
			t.Fatalf("logs diverged at %d:\n%s\n%s", i, logA[i], logB[i])
		}
	}
}

// TestBoardInvariants walks a deterministic match asserting structural
// invariants after every accepted transition, then replays the event log
// to prove chip conservation.
func TestBoardInvariants(t *testing.T) {
	root := Seed{0x0B}
	e, state := testMatchEngine(t, DefaultConfig(), root)
	ids, state := startBoardMatch(t, e, state, "A", "B", "C")

	type snapshot struct {
		state  string
		events []Event
	}
	var history []snapshot
	steps := 0
	for state.Phase == PhasePlaying && steps < 120 {
		p := state.PlayerByID(ids[steps%len(ids)])
		if p == nil || !p.Active() || p.Seat != state.Turn.CurrentSeat {
			// Find the actual current player deterministically.
			found := false
			for _, id := range ids {
				if q := state.PlayerByID(id); q != nil && q.Active() && q.Seat == state.Turn.CurrentSeat {
					p = q
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no eligible current player at tick %d: %+v", state.Tick, state.Turn)
			}
		}
		var events []Event
		switch state.Turn.Phase {
		case TurnAwaitRoll:
			state, events = mustApply(t, e, state, p.ID, RollDiceAction{})
		case TurnAwaitBuyDecision:
			// Alternate buy/decline by tick parity; buy only when legal.
			if state.Tick%2 == 0 {
				if space, err := currentSpace(state, p); err == nil && space.Kind == SpaceProperty && !space.Owned && p.Money >= space.Price {
					state, events = mustApply(t, e, state, p.ID, BuyPropertyAction{})
				} else {
					state, events = mustApply(t, e, state, p.ID, DeclineBuyAction{})
				}
			} else {
				state, events = mustApply(t, e, state, p.ID, DeclineBuyAction{})
			}
		case TurnOver:
			state, events = mustApply(t, e, state, p.ID, EndTurnAction{})
		default:
			t.Fatalf("unknown turn phase %q", state.Turn.Phase)
		}
		history = append(history, snapshot{state: mustJSON(t, state), events: events})
		assertBoardInvariants(t, state)
		steps++
	}
	if steps == 0 {
		t.Fatal("invariant walk applied no transitions")
	}

	// Chip conservation: fold the event log from genesis balances and
	// require exact agreement with the final state.
	balances := map[PlayerID]Money{}
	for _, id := range ids {
		balances[id] = 1500
	}
	holdings := map[PlayerID]map[string]Money{}
	for _, snap := range history {
		for _, ev := range snap.events {
			switch e := ev.(type) {
			case *PropertyBoughtEvent:
				balances[e.PlayerID] -= e.Price
				if holdings[e.PlayerID] == nil {
					holdings[e.PlayerID] = map[string]Money{}
				}
				holdings[e.PlayerID][e.SpaceID] = e.Price
			case *RentPaidEvent:
				balances[e.FromPlayerID] -= e.Amount
				balances[e.ToPlayerID] += e.Amount
			case *BankTransferEvent:
				if e.Direction == "to_player" {
					balances[e.PlayerID] += e.Amount
				} else {
					balances[e.PlayerID] -= e.Amount
				}
			case *PlayerBankruptEvent:
				// Holdings revert to the bank; drop them from the fold.
				delete(holdings, e.PlayerID)
			}
		}
	}
	last := history[len(history)-1].state
	var decoded GameState
	if err := json.Unmarshal([]byte(last), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, p := range decoded.Players {
		if balances[p.ID] != p.Money {
			t.Fatalf("conservation failed for %s: folded %d != state %d", p.ID, balances[p.ID], p.Money)
		}
		wantHoldings := map[string]Money{}
		if h, ok := holdings[p.ID]; ok {
			wantHoldings = h
		}
		gotHoldings := map[string]Money{}
		for _, s := range decoded.Board.Spaces {
			if s.Owned && s.Owner == p.ID {
				gotHoldings[s.ID] = s.Price
			}
		}
		if fmt.Sprintf("%v", wantHoldings) != fmt.Sprintf("%v", gotHoldings) {
			t.Fatalf("holdings mismatch for %s: folded %v != state %v", p.ID, wantHoldings, gotHoldings)
		}
		if p.Status == PlayerEliminated && len(gotHoldings) != 0 {
			t.Fatalf("eliminated player %s still holds %v", p.ID, gotHoldings)
		}
	}
}

func assertBoardInvariants(t *testing.T, state *GameState) {
	t.Helper()
	seen := map[string]bool{}
	for _, p := range state.Players {
		if p.Money < 0 {
			t.Fatalf("negative money for %s: %+v", p.ID, p)
		}
		if p.Position < 0 || (len(state.Board.Spaces) > 0 && p.Position >= len(state.Board.Spaces)) {
			t.Fatalf("position out of bounds: %+v", p)
		}
	}
	for _, s := range state.Board.Spaces {
		if s.Owned {
			if s.Owner == "" {
				t.Fatalf("owned space without owner: %+v", s)
			}
			key := s.ID + ":" + string(s.Owner)
			if seen[key] {
				t.Fatalf("duplicate ownership: %+v", s)
			}
			seen[key] = true
			owner := state.PlayerByID(s.Owner)
			if owner == nil {
				t.Fatalf("owner %q of %q not in match", s.Owner, s.ID)
			}
			if !owner.Active() {
				t.Fatalf("eliminated player %q still owns %q", s.Owner, s.ID)
			}
		}
	}
	if state.Phase == PhasePlaying {
		count := 0
		for _, p := range state.Players {
			if p.Seat == state.Turn.CurrentSeat {
				count++
				if !p.Active() {
					t.Fatalf("current turn owned by ineligible player: %+v", p)
				}
			}
		}
		if count != 1 {
			t.Fatalf("exactly one current player required, found %d", count)
		}
		if !state.Turn.Phase.Valid() {
			t.Fatalf("invalid turn phase %q", state.Turn.Phase)
		}
	}
}
