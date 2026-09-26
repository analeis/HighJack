package game

import (
	"testing"
)

// TestLegalActionsMatchApply is the guarantee that makes LegalActions safe to put
// on the wire.
//
// For a state reached by playing, every action LegalActions advertises must be
// accepted by Apply, and every action it does not advertise must be rejected. That
// is stronger than mirroring the rules: it means a divergence between the
// advertised set and the handlers fails here rather than in a browser, and it
// keeps holding as either side is edited.
func TestLegalActionsMatchApply(t *testing.T) {
	// Probes are keyed by action *type*, so player_ready contributes exactly one:
	// the toggle that would actually change something. The redundant toggle is
	// checked separately below, because it must be rejected rather than merely
	// absent from the advertised set.
	allActions := []Action{
		GameStartAction{},
		RollDiceAction{},
		BuyPropertyAction{},
		DeclineBuyAction{},
		EndTurnAction{},
	}

	engine := func(t *testing.T) (*Engine, *GameState) {
		t.Helper()
		cfg := DefaultConfig()
		e, err := NewEngine(&cfg, Seed{0x0A}, MatchRuleset{})
		if err != nil {
			t.Fatal(err)
		}
		return e, e.NewState("game_legality_test")
	}

	e, state := engine(t)
	cfg := DefaultConfig()

	// The walk is deterministic: a fixed root seed, and a fixed decision about
	// every buy decision, so the same states are visited every run.
	step := func(actor PlayerID, action Action) {
		t.Helper()
		legal := LegalActions(state, &cfg, actor)
		legalSet := map[ActionType]bool{}
		for _, l := range legal {
			legalSet[l] = true
		}

		me := state.PlayerByID(actor)
		if me == nil {
			t.Fatalf("no such actor %s", actor)
		}
		probes := append([]Action{PlayerReadyAction{Ready: !me.Ready}}, allActions...)

		// Probe each action against a clone, before applying the real one, so the
		// advertised set and the engine's own acceptance must agree.
		for _, probe := range probes {
			_, _, err := e.Apply(state.Clone(), actor, probe)
			accepted := err == nil
			if accepted != legalSet[probe.Type()] {
				t.Fatalf("advertised=%v but Apply(%s)=%v (err=%v) in phase=%q turn=%q actor=%s money=%d ready=%v",
					legalSet[probe.Type()], probe.Type(), accepted, err,
					state.Phase, state.Turn.Phase, actor, me.Money, me.Ready)
			}
		}

		// The redundant toggle is never advertised and must never be accepted.
		_, _, err := e.Apply(state.Clone(), actor, PlayerReadyAction{Ready: me.Ready})
		if state.Phase == PhaseLobby && err == nil {
			t.Fatal("a redundant ready toggle must be rejected, not accepted as a no-op")
		}

		next, _, err := e.Apply(state, actor, action)
		if err != nil {
			t.Fatalf("applying a legal action failed: %s: %v", action.Type(), err)
		}
		state = next
	}

	// Lobby: not everyone is ready, so start must not be advertised yet.
	ids, state := joinAndReadyN(t, e, state, "Ace", "Bit")
	_ = ids
	// Both are ready now; the host should be able to start.
	host := state.Host()
	if host == nil {
		t.Fatal("no host")
	}
	assertLegal(t, state, &cfg, host.ID, ActionGameStart, true)
	step(host.ID, GameStartAction{})
	if state.Phase != PhasePlaying {
		t.Fatalf("phase = %q, want playing", state.Phase)
	}

	// Play a long deterministic game, checking the invariant at every step.
	players := make([]PlayerID, 0, len(state.Players))
	for _, p := range state.Players {
		players = append(players, p.ID)
	}
	for i := 0; i < 400 && state.Phase == PhasePlaying; i++ {
		seat := state.Turn.CurrentSeat
		var actor PlayerID
		for _, p := range state.Players {
			if p.Seat == seat {
				actor = p.ID
			}
		}
		if actor == "" {
			t.Fatalf("no player for seat %d", seat)
		}

		// If the current seat holder was eliminated mid-game, advance past them.
		holder := state.PlayerByID(actor)
		if holder == nil || !holder.Active() {
			t.Fatalf("tick %d: current seat %d is not held by an active player", state.Tick, seat)
		}

		// The invariant must also hold for players who are *not* the actor: they may
		// only be offered the right to forfeit. Checked before stepping, because
		// after the step the turn belongs to somebody else and the comparison would
		// be against a state that no longer exists.
		for _, other := range players {
			if other == actor {
				continue
			}
			p := state.PlayerByID(other)
			if p == nil || !p.Active() {
				continue
			}
			for _, l := range LegalActions(state, &cfg, other) {
				if l != ActionPlayerLeave {
					t.Fatalf("tick %d: non-actor %s was offered %s in phase %q (turnSeat=%d actorSeat=%d)",
						state.Tick, other, l, state.Phase, state.Turn.CurrentSeat, holder.Seat)
				}
			}
		}

		var action Action
		switch state.Turn.Phase {
		case TurnAwaitRoll:
			action = RollDiceAction{}
		case TurnAwaitBuyDecision:
			// Buy when affordable, else decline: keeps money interesting and
			// exercises both branches of the decision.
			if space, ok := currentSpaceOf(state, holder); ok &&
				space.Kind == SpaceProperty && !space.Owned && holder.Money >= space.Price {
				action = BuyPropertyAction{}
			} else {
				action = DeclineBuyAction{}
			}
		case TurnOver:
			action = EndTurnAction{}
		default:
			t.Fatalf("unknown turn phase %q", state.Turn.Phase)
		}
		step(actor, action)
	}
}

func assertLegal(t *testing.T, state *GameState, cfg *GameConfig, actor PlayerID, action ActionType, want bool) {
	t.Helper()
	got := false
	for _, l := range LegalActions(state, cfg, actor) {
		if l == action {
			got = true
		}
	}
	if got != want {
		t.Fatalf("LegalActions(%s) contains %s = %v, want %v (phase=%q turn=%q)",
			actor, action, got, want, state.Phase, state.Turn.Phase)
	}
}

func moneyOf(state *GameState, id PlayerID) Money {
	if p := state.PlayerByID(id); p != nil {
		return p.Money
	}
	return 0
}

// joinAndReadyN joins and readies, returning the ids.
func joinAndReadyN(t *testing.T, e *Engine, state *GameState, names ...string) ([]PlayerID, *GameState) {
	t.Helper()
	return joinAndReady(t, e, state, names...)
}

// TestDeclineIsOfferedEvenWhenUnaffordable pins the specific defect: the client's
// "can I afford this" predicate was gating Decline, which the server does not
// gate, so an unaffordable offer left the player with no legal move at all.
func TestDeclineIsOfferedEvenWhenUnaffordable(t *testing.T) {
	cfg := DefaultConfig()
	e, err := NewEngine(&cfg, Seed{0x0A}, MatchRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	state := e.NewState("game_legality_test")
	ids, state := joinAndReady(t, e, state, "Ace", "Bit")
	state, _ = mustApply(t, e, state, ids[0], GameStartAction{})

	// Put the current seat holder on an unowned property, broke.
	state = state.Clone()
	holder := state.PlayerByID(ids[0])
	target := -1
	for i, sp := range state.Board.Spaces {
		if sp.Kind == SpaceProperty && !sp.Owned {
			target = i
			break
		}
	}
	if target < 0 {
		t.Skip("no unowned property on the default board")
	}
	holder.Position = target
	holder.Money = 0
	state.Turn.CurrentSeat = holder.Seat
	state.Turn.Phase = TurnAwaitBuyDecision

	legal := LegalActions(state, &cfg, ids[0])
	hasDecline, hasBuy := false, false
	for _, l := range legal {
		switch l {
		case ActionDeclineBuy:
			hasDecline = true
		case ActionBuyProperty:
			hasBuy = true
		}
	}
	if !hasDecline {
		t.Fatal("decline must be offered whenever the decision is; gating it on " +
			"affordability made a legal action unreachable and could strand the turn")
	}
	if hasBuy {
		t.Fatal("buy must not be offered to a player who cannot afford it")
	}

	// And the engine must actually accept the decline.
	if _, _, err := e.Apply(state.Clone(), ids[0], DeclineBuyAction{}); err != nil {
		t.Fatalf("the engine rejected the decline that was advertised: %v", err)
	}
	// While rejecting the buy.
	if _, _, err := e.Apply(state.Clone(), ids[0], BuyPropertyAction{}); err == nil {
		t.Fatal("the engine accepted a purchase the player cannot afford")
	}
}

// A host who is not ready, or where someone else is not ready, must not be
// offered Start. The client used to enable it on phase+host alone, so the most
// common new-player action was an immediate server rejection.
func TestStartIsOnlyOfferedWhenItWouldSucceed(t *testing.T) {
	cfg := DefaultConfig()
	e, err := NewEngine(&cfg, Seed{0x0A}, MatchRuleset{})
	if err != nil {
		t.Fatal(err)
	}
	state := e.NewState("game_legality_test")

	// One player, alone: below the minimum.
	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "Ace"})
	host := state.Host()
	if host == nil {
		t.Fatal("no host")
	}
	state, _ = mustApply(t, e, state, host.ID, PlayerReadyAction{Ready: true})
	assertLegal(t, state, &cfg, host.ID, ActionGameStart, false)

	// Two ready players: now it is permitted.
	state, _ = mustApply(t, e, state, "", PlayerJoinAction{DisplayName: "Bit"})
	assertLegal(t, state, &cfg, host.ID, ActionGameStart, false) // Bit not ready
	bit := state.Players[1]
	state, _ = mustApply(t, e, state, bit.ID, PlayerReadyAction{Ready: true})
	assertLegal(t, state, &cfg, host.ID, ActionGameStart, true)

	// A non-host is never offered it.
	assertLegal(t, state, &cfg, bit.ID, ActionGameStart, false)
}
