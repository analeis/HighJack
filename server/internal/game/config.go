package game

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ConfigSchemaVersion must match SCHEMA_VERSION in packages/protocol.
// Bump both together; fixture tests on both sides enforce equality.
const ConfigSchemaVersion = 1

// VictoryType selects how a match is decided.
type VictoryType string

const (
	VictoryLastStanding VictoryType = "last_standing"
	VictoryTargetWealth VictoryType = "target_wealth"
	VictoryRoundLimit   VictoryType = "round_limit"
)

// Hard limits shared with packages/protocol (LIMITS).
const (
	MinPlayersFloor   = 2
	MaxPlayersCeiling = 12
	StartingMoneyMax  = 1_000_000
)

type PlayerCountRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type TradingConfig struct {
	Enabled bool `json:"enabled"`
}

type AuctionsConfig struct {
	Enabled bool `json:"enabled"`
}

type CarnivalConfig struct {
	Enabled bool `json:"enabled"`
}

type SportsConfig struct {
	Enabled bool `json:"enabled"`
}

type CardsConfig struct {
	Enabled bool `json:"enabled"`
}

type GamblingConfig struct {
	Enabled   bool `json:"enabled"`
	Poker     bool `json:"poker"`
	Blackjack bool `json:"blackjack"`
	Casino    bool `json:"casino"`
}

type RandomEventsConfig struct {
	Enabled       bool `json:"enabled"`
	IntervalTicks int  `json:"intervalTicks"`
}

type VictoryConfig struct {
	Type         VictoryType `json:"type"`
	TargetWealth int64       `json:"targetWealth"`
	RoundLimit   int         `json:"roundLimit"`
}

// GameConfig is a first-class domain object: a HighJack match is a
// configurable ruleset, and this structure is that ruleset's canonical
// representation. Fields for systems that are not implemented yet are
// modeled now so configuration tooling, storage, and protocol handling
// stay stable while gameplay arrives feature by feature. Unimplemented
// subsystems never affect simulation output.
//
// The JSON shape mirrors GameConfig in packages/protocol/src/config.ts;
// fixture tests in both languages keep them byte-compatible.
type GameConfig struct {
	Version       int                `json:"version"`
	PlayerCount   PlayerCountRange   `json:"playerCount"`
	StartingMoney Money              `json:"startingMoney"`
	Trading       TradingConfig      `json:"trading"`
	Auctions      AuctionsConfig     `json:"auctions"`
	Gambling      GamblingConfig     `json:"gambling"`
	Carnival      CarnivalConfig     `json:"carnival"`
	Sports        SportsConfig       `json:"sports"`
	Cards         CardsConfig        `json:"cards"`
	RandomEvents  RandomEventsConfig `json:"randomEvents"`
	Victory       VictoryConfig      `json:"victory"`
}

// DefaultConfig returns the baseline configuration used by the server and
// mirrored by DEFAULT_CONFIG in packages/protocol.
func DefaultConfig() GameConfig {
	return GameConfig{
		Version:       ConfigSchemaVersion,
		PlayerCount:   PlayerCountRange{Min: MinPlayersFloor, Max: 8},
		StartingMoney: 1500,
		Trading:       TradingConfig{Enabled: true},
		Auctions:      AuctionsConfig{Enabled: true},
		Gambling:      GamblingConfig{},
		Carnival:      CarnivalConfig{},
		Sports:        SportsConfig{},
		Cards:         CardsConfig{Enabled: true},
		RandomEvents:  RandomEventsConfig{},
		Victory:       VictoryConfig{Type: VictoryLastStanding},
	}
}

// CanonicalJSON produces the deterministic serialization used for hashing:
// object keys sorted lexicographically, no whitespace, no HTML escaping,
// numbers preserved exactly. Identical logic exists in TypeScript
// (canonicalJson in packages/protocol); both must agree byte-for-byte.
func (c *GameConfig) CanonicalJSON() ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("re-decode config: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, fmt.Errorf("encode canonical config: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Hash returns the lowercase hex sha256 of the canonical JSON form.
func (c *GameConfig) Hash() (string, error) {
	canonical, err := c.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
