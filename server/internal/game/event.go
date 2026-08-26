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
