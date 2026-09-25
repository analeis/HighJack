package game

import "fmt"

// TurnPhase names the stage of the current player's turn inside
// PhasePlaying. It is turn-local state, not a match lifecycle phase: the
// match phase graph (lobby → playing → ended) is untouched by v0.2.
type TurnPhase string

const (
	TurnAwaitRoll        TurnPhase = "await_roll"
	TurnAwaitBuyDecision TurnPhase = "await_buy_decision"
	TurnOver             TurnPhase = "turn_over"
)

func (p TurnPhase) Valid() bool {
	switch p {
	case TurnAwaitRoll, TurnAwaitBuyDecision, TurnOver:
		return true
	default:
		return false
	}
}

// TurnState is the authoritative turn pointer. Round starts at 1 and
// increments when a turn passes to a lower seat number (seat-order wrap).
type TurnState struct {
	CurrentSeat    Seat      `json:"currentSeat"`
	Phase          TurnPhase `json:"phase"`
	Round          int       `json:"round"`
	DoublesStreak  int       `json:"doublesStreak"`
	AwardExtraRoll bool      `json:"awardExtraRoll"`
}

// RuntimeSpace is one board space with live ownership. Ownership is
// canonical here and nowhere else: players do not carry property lists.
// Owner is meaningful only when Owned is true; storing a value (not a
// pointer) keeps GameState.Clone a true deep copy.
type RuntimeSpace struct {
	ID     string    `json:"id"`
	Kind   SpaceKind `json:"kind"`
	Name   string    `json:"name"`
	Group  string    `json:"group"`
	Price  Money     `json:"price"`
	Rent   Money     `json:"rent"`
	Amount Money     `json:"amount"`
	Owned  bool      `json:"owned"`
	Owner  PlayerID  `json:"owner"`
	Level  int       `json:"level"`
}

// BoardState is the live board: configured spaces in deterministic order
// plus ownership. Built once from validated config at match start.
type BoardState struct {
	Spaces []RuntimeSpace `json:"spaces"`
}

// newBoardRuntime builds the pristine board from validated configuration.
// All spaces start unowned at level 0; every player starts on space 0.
func newBoardRuntime(cfg *GameConfig) BoardState {
	spaces := make([]RuntimeSpace, len(cfg.Board.Spaces))
	for i, s := range cfg.Board.Spaces {
		spaces[i] = RuntimeSpace{
			ID: s.ID, Kind: s.Kind, Name: s.Name, Group: s.Group,
			Price: s.Price, Rent: s.Rent, Amount: s.Amount,
		}
	}
	return BoardState{Spaces: spaces}
}

// spaceByIndex returns the space at a board position. Positions are always
// kept modulo len(spaces) by movement, so an out-of-range index indicates
// corrupted state.
func (b BoardState) spaceByIndex(pos int) (RuntimeSpace, error) {
	if len(b.Spaces) == 0 || pos < 0 || pos >= len(b.Spaces) {
		return RuntimeSpace{}, fmt.Errorf("game: board position %d out of range", pos)
	}
	return b.Spaces[pos], nil
}

// PropertiesOf returns the spaces owned by a player in board order.
func PropertiesOf(state *GameState, id PlayerID) []RuntimeSpace {
	var out []RuntimeSpace
	for _, s := range state.Board.Spaces {
		if s.Owned && s.Owner == id {
			out = append(out, s)
		}
	}
	return out
}

// NetWorth values a player as money plus the purchase price of every
// holding. There is no resale market in v0.2, so purchase price is the
// single documented valuation rule.
func NetWorth(state *GameState, id PlayerID) Money {
	p := state.PlayerByID(id)
	if p == nil {
		return 0
	}
	total := p.Money
	for _, s := range PropertiesOf(state, id) {
		total += s.Price
	}
	return total
}

// richestActive returns the active player with the highest net worth,
// breaking ties by lowest seat so victory is deterministic.
func richestActive(state *GameState) *Player {
	var best *Player
	var bestWorth Money
	for i := range state.Players {
		p := &state.Players[i]
		if !p.Active() {
			continue
		}
		w := NetWorth(state, p.ID)
		if best == nil || w > bestWorth {
			best, bestWorth = p, w
		}
	}
	return best
}

// advanceSeat computes the next eligible seat in seat order, skipping
// eliminated players, and the resulting round (incremented on wrap).
// Callers must have verified at least one active player remains.
func advanceSeat(state *GameState, from Seat) (Seat, int) {
	round := state.Turn.Round
	var fallback *Player
	for _, p := range state.ActivePlayers() {
		if fallback == nil {
			fallback = &p
		}
		if p.Seat > from {
			return p.Seat, round
		}
	}
	// Wrap: no higher active seat, so the turn returns to the lowest one
	// and a new round begins.
	if fallback != nil {
		return fallback.Seat, round + 1
	}
	return from, round
}

// evaluateMatchEnd checks every configured victory condition after an
// accepted playing-phase transition. It is a no-op outside PhasePlaying
// or when no condition triggers. Priority: last_standing, then
// target_wealth, then round_limit.
func evaluateMatchEnd(next *GameState, cfg *GameConfig) ([]Event, error) {
	if next.Phase != PhasePlaying {
		return nil, nil
	}
	end := func(winner *PlayerID, reason string) ([]Event, error) {
		if err := next.Phase.canTransitionTo(PhaseEnded); err != nil {
			return nil, err
		}
		next.Phase = PhaseEnded
		next.WinnerID = winner
		next.EndReason = reason
		return []Event{&GameEndedEvent{WinnerID: winner, Reason: reason}}, nil
	}

	if len(next.ActivePlayers()) < 2 {
		return maybeEndForInsufficientPlayers(next)
	}
	active := next.ActivePlayers()
	if cfg.Victory.Type == VictoryTargetWealth {
		for _, p := range active {
			if NetWorth(next, p.ID) >= Money(cfg.Victory.TargetWealth) {
				winner := richestActive(next)
				if winner == nil {
					return end(nil, "target_wealth")
				}
				winnerID := winner.ID
				return end(&winnerID, "target_wealth")
			}
		}
	}
	if cfg.Victory.Type == VictoryRoundLimit && next.Turn.Round > cfg.Victory.RoundLimit {
		winner := richestActive(next)
		if winner == nil {
			return end(nil, "round_limit")
		}
		winnerID := winner.ID
		return end(&winnerID, "round_limit")
	}
	return nil, nil
}
