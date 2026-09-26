package game

// EventType names a fact emitted by the engine.
type EventType string

const (
	EventPlayerJoined       EventType = "player_joined"
	EventPlayerLeft         EventType = "player_left"
	EventPlayerReadyChanged EventType = "player_ready_changed"
	EventGameStarted        EventType = "game_started"
	EventPlayerEliminated   EventType = "player_eliminated"
	EventGameEnded          EventType = "game_ended"
	EventDiceRolled         EventType = "dice_rolled"
	EventPropertyBought     EventType = "property_bought"
	EventBuyDeclined        EventType = "buy_declined"
	EventRentPaid           EventType = "rent_paid"
	EventBankTransfer       EventType = "bank_transfer"
	EventPlayerBankrupt     EventType = "player_bankrupt"
	EventTurnAdvanced       EventType = "turn_advanced"
)

// Event is a fact about a state transition. Events describe what changed;
// they never command. Every event records the tick that produced it, and
// none carries wall-clock time, so event logs are deterministic and
// replayable.
//
// JSON field names mirror packages/protocol/src/events.ts; fixture tests on
// both sides keep the wire shapes byte-compatible.
type Event interface {
	Type() EventType
	Tick() uint64
}

type eventBase struct{ tick uint64 }

func (e eventBase) Tick() uint64 { return e.tick }

type PlayerJoinedEvent struct {
	eventBase
	PlayerID      PlayerID `json:"playerId"`
	Name          string   `json:"name"`
	Seat          Seat     `json:"seat"`
	StartingMoney Money    `json:"startingMoney"`
}

func (PlayerJoinedEvent) Type() EventType { return EventPlayerJoined }

type PlayerLeftEvent struct {
	eventBase
	PlayerID PlayerID `json:"playerId"`
	Reason   string   `json:"reason"` // "voluntary" | "disconnected"
}

func (PlayerLeftEvent) Type() EventType { return EventPlayerLeft }

type PlayerReadyChangedEvent struct {
	eventBase
	PlayerID PlayerID `json:"playerId"`
	Ready    bool     `json:"ready"`
}

func (PlayerReadyChangedEvent) Type() EventType { return EventPlayerReadyChanged }

type GameStartedEvent struct {
	eventBase
	ConfigHash string `json:"configHash"`
	Seed       string `json:"seed"` // hex-encoded deterministic match seed
}

func (GameStartedEvent) Type() EventType { return EventGameStarted }

type PlayerEliminatedEvent struct {
	eventBase
	PlayerID PlayerID `json:"playerId"`
	Cause    string   `json:"cause"`
}

func (PlayerEliminatedEvent) Type() EventType { return EventPlayerEliminated }

type GameEndedEvent struct {
	eventBase
	WinnerID *PlayerID `json:"winnerId"`
	Reason   string    `json:"reason"`
}

func (GameEndedEvent) Type() EventType { return EventGameEnded }

type DiceRolledEvent struct {
	eventBase
	PlayerID  PlayerID `json:"playerId"`
	Die1      int      `json:"die1"`
	Die2      int      `json:"die2"`
	FromSpace string   `json:"fromSpace"`
	ToSpace   string   `json:"toSpace"`
	PassedGo  bool     `json:"passedGo"`
}

func (DiceRolledEvent) Type() EventType { return EventDiceRolled }

type PropertyBoughtEvent struct {
	eventBase
	PlayerID PlayerID `json:"playerId"`
	SpaceID  string   `json:"spaceId"`
	Price    Money    `json:"price"`
}

func (PropertyBoughtEvent) Type() EventType { return EventPropertyBought }

type BuyDeclinedEvent struct {
	eventBase
	PlayerID PlayerID `json:"playerId"`
	SpaceID  string   `json:"spaceId"`
}

func (BuyDeclinedEvent) Type() EventType { return EventBuyDeclined }

type RentPaidEvent struct {
	eventBase
	FromPlayerID PlayerID `json:"fromPlayerId"`
	ToPlayerID   PlayerID `json:"toPlayerId"`
	SpaceID      string   `json:"spaceId"`
	Amount       Money    `json:"amount"`
}

func (RentPaidEvent) Type() EventType { return EventRentPaid }

// BankTransfer records an explicit bank flow so chip conservation stays
// auditable: the infinite bank issues and absorbs chips only through this
// event (reasons "pass_go", "land_go", "tax").
type BankTransferEvent struct {
	eventBase
	PlayerID  PlayerID `json:"playerId"`
	Amount    Money    `json:"amount"`
	Direction string   `json:"direction"` // "to_player" | "to_bank"
	Reason    string   `json:"reason"`
}

func (BankTransferEvent) Type() EventType { return EventBankTransfer }

type PlayerBankruptEvent struct {
	eventBase
	PlayerID   PlayerID `json:"playerId"`
	Cause      string   `json:"cause"`
	CreditorID PlayerID `json:"creditorId,omitempty"`
	AmountOwed Money    `json:"amountOwed"`
	// ReleasedSpaces lists the board spaces that reverted to the bank. Without
	// it the bank absorbs property value with no event naming it, so the log
	// cannot explain the post-state and a replay cannot reconstruct ownership.
	ReleasedSpaces []string `json:"releasedSpaces,omitempty"`
}

func (PlayerBankruptEvent) Type() EventType { return EventPlayerBankrupt }

type TurnAdvancedEvent struct {
	eventBase
	Seat  Seat `json:"seat"`
	Round int  `json:"round"`
}

func (TurnAdvancedEvent) Type() EventType { return EventTurnAdvanced }

// AllEventTypes returns every event type on the wire. It exists for the
// cross-language parity test in internal/protocol.
func AllEventTypes() []EventType {
	return []EventType{
		EventPlayerJoined,
		EventPlayerLeft,
		EventPlayerReadyChanged,
		EventGameStarted,
		EventPlayerEliminated,
		EventGameEnded,
		EventDiceRolled,
		EventBankTransfer,
		EventPropertyBought,
		EventBuyDeclined,
		EventRentPaid,
		EventPlayerBankrupt,
		EventTurnAdvanced,
	}
}
