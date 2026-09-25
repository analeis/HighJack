package protocol

import (
	"github.com/analeis/highjack/server/internal/game"
)

// Snapshot wire shapes. These mirror GameSnapshot in
// packages/protocol/src/state.ts exactly (field names, nullability, and
// integer types); shared fixtures pin the two sides together.

type SnapshotPlayer struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Seat     int    `json:"seat"`
	Money    int64  `json:"money"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	IsHost   bool   `json:"isHost"`
	Position int    `json:"position"`
}

type SnapshotSpace struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Group  string `json:"group"`
	Price  int64  `json:"price"`
	Rent   int64  `json:"rent"`
	Amount int64  `json:"amount"`
	Owned  bool   `json:"owned"`
	Owner  string `json:"owner"`
	Level  int    `json:"level"`
}

type SnapshotTurn struct {
	CurrentSeat    int    `json:"currentSeat"`
	Phase          string `json:"phase"`
	Round          int    `json:"round"`
	DoublesStreak  int    `json:"doublesStreak"`
	AwardExtraRoll bool   `json:"awardExtraRoll"`
}

type GameSnapshot struct {
	MatchID    string           `json:"matchId"`
	Phase      string           `json:"phase"`
	Tick       uint64           `json:"tick"`
	Players    []SnapshotPlayer `json:"players"`
	Spaces     []SnapshotSpace  `json:"spaces"`
	Turn       SnapshotTurn     `json:"turn"`
	WinnerID   *string          `json:"winnerId"`
	EndReason  string           `json:"endReason"`
	ConfigHash string           `json:"configHash"`
}

// NewGameSnapshot projects authoritative engine state into the client
// wire shape. The server root seed is never included: it is replay/debug
// metadata, not client-visible state.
func NewGameSnapshot(state *game.GameState) GameSnapshot {
	snap := GameSnapshot{
		MatchID:    string(state.ID),
		Phase:      string(state.Phase),
		Tick:       state.Tick,
		Players:    make([]SnapshotPlayer, 0, len(state.Players)),
		Spaces:     make([]SnapshotSpace, 0, len(state.Board.Spaces)),
		WinnerID:   nil,
		EndReason:  state.EndReason,
		ConfigHash: state.ConfigHash,
	}
	for _, p := range state.Players {
		snap.Players = append(snap.Players, SnapshotPlayer{
			PlayerID: string(p.ID),
			Name:     p.Name,
			Seat:     int(p.Seat),
			Money:    int64(p.Money),
			Status:   string(p.Status),
			Ready:    p.Ready,
			IsHost:   p.IsHost,
			Position: p.Position,
		})
	}
	for _, s := range state.Board.Spaces {
		snap.Spaces = append(snap.Spaces, SnapshotSpace{
			ID: s.ID, Kind: string(s.Kind), Name: s.Name, Group: s.Group,
			Price: int64(s.Price), Rent: int64(s.Rent), Amount: int64(s.Amount),
			Owned: s.Owned, Owner: string(s.Owner), Level: s.Level,
		})
	}
	snap.Turn = SnapshotTurn{
		CurrentSeat:    int(state.Turn.CurrentSeat),
		Phase:          string(state.Turn.Phase),
		Round:          state.Turn.Round,
		DoublesStreak:  state.Turn.DoublesStreak,
		AwardExtraRoll: state.Turn.AwardExtraRoll,
	}
	if state.WinnerID != nil {
		winner := string(*state.WinnerID)
		snap.WinnerID = &winner
	}
	return snap
}
