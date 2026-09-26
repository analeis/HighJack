package game

import "fmt"

// BoardRuleset implements the v0.2 board loop: deterministic dice,
// movement, landing resolution, purchase decisions, rent, tax, bank
// accounting, bankruptcy, turn advancement, and victory evaluation.
//
// One accepted action performs exactly one atomic transition. A roll
// resolves its entire landing (including rent, tax, bonuses, and any
// bankruptcy) inside the same transition so a landing is never left
// partially resolved. Every roll consumes exactly two RNG draws, in fixed
// die order, regardless of outcome.
type BoardRuleset struct{}

func (BoardRuleset) Name() string { return "board/v1" }

func (r BoardRuleset) Handle(state *GameState, cfg *GameConfig, actor PlayerID, action Action, rng Rng) (*GameState, []Event, error) {
	switch action.(type) {
	case RollDiceAction:
		return r.roll(state, cfg, actor, rng)
	case BuyPropertyAction:
		return r.buy(state, cfg, actor)
	case DeclineBuyAction:
		return r.decline(state, cfg, actor)
	case EndTurnAction:
		return r.endTurn(state, cfg, actor)
	default:
		return nil, nil, ErrUnknownAction
	}
}

// requireTurn validates the shared preconditions of every board action:
// an active match, an active actor who owns the current turn, a sane turn
// phase, and an initialized board.
func requireTurn(state *GameState, actor PlayerID, phase TurnPhase) (*Player, error) {
	if state.Phase != PhasePlaying {
		return nil, ErrOutOfPhase
	}
	if !state.Turn.Phase.Valid() {
		return nil, fmt.Errorf("game: corrupt turn phase %q", state.Turn.Phase)
	}
	if len(state.Board.Spaces) == 0 {
		return nil, fmt.Errorf("game: match board not initialized")
	}
	p := state.PlayerByID(actor)
	if p == nil {
		return nil, ErrPlayerNotInGame
	}
	if !p.Active() {
		return nil, ErrNotPermitted
	}
	// The position indexes Board.Spaces directly, and every board action reads
	// it. A state that reached this point with an out-of-range position — a
	// corrupt snapshot, say — would panic inside the transition, on the
	// goroutine holding the match lock, wedging the match permanently. Refusing
	// here turns that into a reportable error.
	if p.Position < 0 || int(p.Position) >= len(state.Board.Spaces) {
		return nil, fmt.Errorf("game: player %q position %d is outside the board (%d spaces)",
			actor, p.Position, len(state.Board.Spaces))
	}
	if p.Seat != state.Turn.CurrentSeat {
		return nil, ErrNotYourTurn
	}
	if state.Turn.Phase != phase {
		return nil, ErrOutOfPhase
	}
	return p, nil
}

func (r BoardRuleset) roll(state *GameState, cfg *GameConfig, actor PlayerID, rng Rng) (*GameState, []Event, error) {
	p, err := requireTurn(state, actor, TurnAwaitRoll)
	if err != nil {
		return nil, nil, err
	}

	d1 := rng.IntN(6) + 1
	d2 := rng.IntN(6) + 1
	doubles := d1 == d2

	streak := state.Turn.DoublesStreak
	extra := false
	if doubles && cfg.PropertyRules.DoublesExtraRoll {
		streak++
		extra = streak < cfg.PropertyRules.MaxDoublesStreak
	} else {
		streak = 0
	}
	// A capped doubles streak resolves the roll normally and then passes
	// the turn: no extra roll, streak reset, uniform RNG consumption.

	n := len(state.Board.Spaces)
	total := d1 + d2
	to := (p.Position + total) % n
	passes := (p.Position + total) / n

	next := state.Clone()
	np := next.PlayerByID(actor)
	np.Position = to

	events := []Event{
		&DiceRolledEvent{
			PlayerID:  actor,
			Die1:      d1,
			Die2:      d2,
			FromSpace: state.Board.Spaces[p.Position].ID,
			ToSpace:   next.Board.Spaces[to].ID,
			PassedGo:  passes > 0,
		},
	}

	if passes > 0 {
		reason := "pass_go"
		if to == 0 {
			reason = "land_go" // passing and landing never stack
		}
		np.Money += cfg.PropertyRules.PassingGoBonus * Money(passes)
		events = append(events, &BankTransferEvent{
			PlayerID:  actor,
			Amount:    cfg.PropertyRules.PassingGoBonus * Money(passes),
			Direction: "to_player",
			Reason:    reason,
		})
	}

	landing, err := r.resolveLanding(next, cfg, actor, to)
	if err != nil {
		return nil, nil, err
	}
	events = append(events, landing.events...)

	// Bankruptcy or match end short-circuits the normal turn flow; the
	// resolution path already advanced or ended the match.
	if landing.bankrupt || next.Phase != PhasePlaying {
		endEv, err := evaluateMatchEnd(next, cfg)
		if err != nil {
			return nil, nil, err
		}
		return next, append(events, endEv...), nil
	}

	switch {
	case landing.buyDecision:
		next.Turn.Phase = TurnAwaitBuyDecision
		next.Turn.DoublesStreak = streak
		next.Turn.AwardExtraRoll = extra
	case extra:
		next.Turn.Phase = TurnAwaitRoll
		next.Turn.DoublesStreak = streak
		next.Turn.AwardExtraRoll = false
	default:
		next.Turn.Phase = TurnOver
		next.Turn.DoublesStreak = 0
		next.Turn.AwardExtraRoll = false
	}

	endEv, err := evaluateMatchEnd(next, cfg)
	if err != nil {
		return nil, nil, err
	}
	return next, append(events, endEv...), nil
}

// landingOutcome describes what a landing resolution did.
type landingOutcome struct {
	events      []Event
	buyDecision bool // turn must await a purchase decision
	bankrupt    bool // actor went insolvent; turn already advanced/ended
}

// resolveLanding applies the destination space effect to next (already
// moved). It never leaves a mandatory payment half-applied: unaffordable
// payments route to bankruptcy atomically.
func (r BoardRuleset) resolveLanding(next *GameState, cfg *GameConfig, actor PlayerID, to int) (landingOutcome, error) {
	np := next.PlayerByID(actor)
	if to < 0 || to >= len(next.Board.Spaces) {
		return landingOutcome{}, fmt.Errorf("game: board position %d out of range", to)
	}
	space := &next.Board.Spaces[to]
	dest := *space
	switch dest.Kind {
	case SpaceGo, SpaceNeutral:
		return landingOutcome{}, nil
	case SpaceTax:
		if np.Money < dest.Amount {
			return r.bankrupt(next, cfg, actor, "", dest.Amount, "bankruptcy_tax")
		}
		np.Money -= dest.Amount
		return landingOutcome{events: []Event{&BankTransferEvent{
			PlayerID:  actor,
			Amount:    dest.Amount,
			Direction: "to_bank",
			Reason:    "tax",
		}}}, nil
	case SpaceProperty:
		switch {
		case !space.Owned:
			if np.Money >= space.Price {
				return landingOutcome{buyDecision: true}, nil
			}
			return landingOutcome{}, nil // cannot afford: no decision offered
		case space.Owner == actor:
			return landingOutcome{}, nil // own holding: quiet landing
		default:
			owner := next.PlayerByID(space.Owner)
			if owner == nil || !owner.Active() {
				return landingOutcome{}, nil // defensive: ownership invariant says unreachable
			}
			if np.Money < space.Rent {
				return r.bankrupt(next, cfg, actor, owner.ID, space.Rent, "bankruptcy_rent")
			}
			np.Money -= space.Rent
			owner.Money += space.Rent
			return landingOutcome{events: []Event{&RentPaidEvent{
				FromPlayerID: actor,
				ToPlayerID:   owner.ID,
				SpaceID:      space.ID,
				Amount:       space.Rent,
			}}}, nil
		}
	}
	return landingOutcome{}, fmt.Errorf("game: unresolvable space kind %q", dest.Kind)
}

// bankrupt eliminates the actor atomically: holdings revert to the bank,
// the player is eliminated through the lifecycle path, and the turn
// auto-advances past them (or the match end is left to evaluation).
// releaseHoldings returns every space owned by a player to the bank, unowned,
// and reports the space ids so the change is reconstructible from the event log.
//
// Both elimination paths must go through here. When only the bankruptcy path
// swept, a player who left mid-match kept their properties while eliminated:
// those spaces could never be bought (buy and decline both reject an owned
// space) and could never charge rent (the payer is skipped when the owner is
// not active), so they were dead, rent-free and unpurchasable for the rest of
// the match, with no event recording the change.
func releaseHoldings(state *GameState, owner PlayerID) []string {
	var released []string
	for i := range state.Board.Spaces {
		if state.Board.Spaces[i].Owned && state.Board.Spaces[i].Owner == owner {
			state.Board.Spaces[i].Owned = false
			state.Board.Spaces[i].Owner = ""
			state.Board.Spaces[i].Level = 0
			released = append(released, state.Board.Spaces[i].ID)
		}
	}
	return released
}

func (r BoardRuleset) bankrupt(next *GameState, cfg *GameConfig, actor, creditor PlayerID, owed Money, cause string) (landingOutcome, error) {
	np := next.PlayerByID(actor)
	released := releaseHoldings(next, actor)
	np.Status = PlayerEliminated
	out := landingOutcome{bankrupt: true}
	out.events = append(out.events,
		&PlayerBankruptEvent{
			PlayerID: actor, Cause: cause, CreditorID: creditor,
			AmountOwed: owed, ReleasedSpaces: released,
		},
		&PlayerEliminatedEvent{PlayerID: actor, Cause: cause},
	)
	if len(next.ActivePlayers()) == 0 {
		return out, nil // evaluation records the empty-table end
	}
	seat, round := advanceSeat(next, np.Seat)
	next.Turn.CurrentSeat = seat
	next.Turn.Round = round
	next.Turn.Phase = TurnAwaitRoll
	next.Turn.DoublesStreak = 0
	next.Turn.AwardExtraRoll = false
	out.events = append(out.events, &TurnAdvancedEvent{Seat: seat, Round: round})
	return out, nil
}

func (r BoardRuleset) buy(state *GameState, cfg *GameConfig, actor PlayerID) (*GameState, []Event, error) {
	p, err := requireTurn(state, actor, TurnAwaitBuyDecision)
	if err != nil {
		return nil, nil, err
	}
	next := state.Clone()
	space, err := currentSpace(next, p)
	if err != nil {
		return nil, nil, err
	}
	if space.Kind != SpaceProperty || space.Owned {
		return nil, nil, ErrNotPermitted
	}
	np := next.PlayerByID(actor)
	if np.Money < space.Price {
		return nil, nil, ErrNotPermitted
	}
	np.Money -= space.Price
	space.Owned = true
	space.Owner = actor

	events := []Event{&PropertyBoughtEvent{PlayerID: actor, SpaceID: space.ID, Price: space.Price}}
	if next.Turn.AwardExtraRoll {
		next.Turn.Phase = TurnAwaitRoll
	} else {
		next.Turn.Phase = TurnOver
	}
	next.Turn.AwardExtraRoll = false

	endEv, err := evaluateMatchEnd(next, cfg)
	if err != nil {
		return nil, nil, err
	}
	return next, append(events, endEv...), nil
}

func (r BoardRuleset) decline(state *GameState, cfg *GameConfig, actor PlayerID) (*GameState, []Event, error) {
	p, err := requireTurn(state, actor, TurnAwaitBuyDecision)
	if err != nil {
		return nil, nil, err
	}
	next := state.Clone()
	space, err := currentSpace(next, p)
	if err != nil {
		return nil, nil, err
	}
	if space.Kind != SpaceProperty || space.Owned {
		return nil, nil, ErrNotPermitted
	}
	// Declining is terminal and explicit — not an auction, not a pass.
	events := []Event{&BuyDeclinedEvent{PlayerID: actor, SpaceID: space.ID}}
	if next.Turn.AwardExtraRoll {
		next.Turn.Phase = TurnAwaitRoll
	} else {
		next.Turn.Phase = TurnOver
	}
	next.Turn.AwardExtraRoll = false

	endEv, err := evaluateMatchEnd(next, cfg)
	if err != nil {
		return nil, nil, err
	}
	return next, append(events, endEv...), nil
}

func (r BoardRuleset) endTurn(state *GameState, cfg *GameConfig, actor PlayerID) (*GameState, []Event, error) {
	if _, err := requireTurn(state, actor, TurnOver); err != nil {
		return nil, nil, err
	}
	next := state.Clone()
	seat, round := advanceSeat(next, state.Turn.CurrentSeat)
	next.Turn.CurrentSeat = seat
	next.Turn.Round = round
	next.Turn.Phase = TurnAwaitRoll
	next.Turn.DoublesStreak = 0
	next.Turn.AwardExtraRoll = false

	events := []Event{&TurnAdvancedEvent{Seat: seat, Round: round}}
	endEv, err := evaluateMatchEnd(next, cfg)
	if err != nil {
		return nil, nil, err
	}
	return next, append(events, endEv...), nil
}

// currentSpace resolves the actor's position to a mutable runtime space.
func currentSpace(next *GameState, p *Player) (*RuntimeSpace, error) {
	if p.Position < 0 || p.Position >= len(next.Board.Spaces) {
		return nil, fmt.Errorf("game: player position %d out of range", p.Position)
	}
	return &next.Board.Spaces[p.Position], nil
}
