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

	MinBoardSpaces = 2
	MaxBoardSpaces = 64
	MaxSpaceIDLen  = 32
	MaxSpaceName   = 64
	MaxGroupLen    = 32
	MaxSpacePrice  = 1_000_000
	MaxSpaceRent   = 100_000
	MaxTaxAmount   = 1_000_000
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

// SpaceKind names the mechanical category of a board space.
type SpaceKind string

const (
	SpaceGo       SpaceKind = "go"
	SpaceProperty SpaceKind = "property"
	SpaceTax      SpaceKind = "tax"
	SpaceNeutral  SpaceKind = "neutral"
)

// BoardSpace is one configured board space. All fields are always present
// in the canonical JSON form (zero values for non-applicable kinds) so Go
// and TypeScript serializations stay byte-identical.
type BoardSpace struct {
	ID     string    `json:"id"`
	Kind   SpaceKind `json:"kind"`
	Name   string    `json:"name"`
	Group  string    `json:"group"`
	Price  Money     `json:"price"`
	Rent   Money     `json:"rent"`
	Amount Money     `json:"amount"`
}

type BoardConfig struct {
	Spaces []BoardSpace `json:"spaces"`
}

type PropertyRules struct {
	PassingGoBonus   Money `json:"passingGoBonus"`
	DoublesExtraRoll bool  `json:"doublesExtraRoll"`
	MaxDoublesStreak int   `json:"maxDoublesStreak"`
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
	Board         BoardConfig        `json:"board"`
	PropertyRules PropertyRules      `json:"propertyRules"`
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
		Board:         BoardConfig{Spaces: DefaultBoard()},
		PropertyRules: PropertyRules{PassingGoBonus: 200, DoublesExtraRoll: true, MaxDoublesStreak: 3},
	}
}

// DefaultBoard returns the standard 24-space loop used when no custom
// board is configured. Movement is clockwise (increasing index) with
// wraparound; space 0 is always go. Mirrored by DEFAULT_BOARD in
// packages/protocol; the canonical-hash fixture pins parity.
func DefaultBoard() []BoardSpace {
	prop := func(id, name, group string, price, rent Money) BoardSpace {
		return BoardSpace{ID: id, Kind: SpaceProperty, Name: name, Group: group, Price: price, Rent: rent}
	}
	return []BoardSpace{
		{ID: "go", Kind: SpaceGo, Name: "Start"},
		prop("a1", "Copper Row", "Copper", 100, 10),
		prop("a2", "Tin Lane", "Copper", 120, 12),
		{ID: "n1", Kind: SpaceNeutral, Name: "Old Fountain"},
		{ID: "t1", Kind: SpaceTax, Name: "Toll Gate", Amount: 75},
		prop("a3", "Brass Way", "Copper", 140, 14),
		prop("a4", "Nickel Court", "Copper", 160, 16),
		{ID: "n2", Kind: SpaceNeutral, Name: "Night Market"},
		prop("b1", "Lantern Row", "Lantern", 180, 18),
		prop("b2", "Wick Street", "Lantern", 200, 20),
		{ID: "t2", Kind: SpaceTax, Name: "Harbor Toll", Amount: 100},
		prop("b3", "Glow Alley", "Lantern", 220, 22),
		prop("b4", "Beacon Court", "Lantern", 240, 24),
		{ID: "n3", Kind: SpaceNeutral, Name: "Grand Plaza"},
		prop("c1", "Dockside Row", "Harbor", 260, 26),
		prop("c2", "Anchor Lane", "Harbor", 280, 28),
		{ID: "t3", Kind: SpaceTax, Name: "Crown Tax", Amount: 150},
		prop("c3", "Tideway", "Harbor", 300, 30),
		prop("c4", "Lighthouse Point", "Harbor", 320, 32),
		{ID: "n4", Kind: SpaceNeutral, Name: "Sky Garden"},
		prop("d1", "Summit Rise", "Summit", 340, 34),
		prop("d2", "Cloud Terrace", "Summit", 360, 36),
		prop("d3", "Peak View", "Summit", 380, 38),
		prop("d4", "Crown Heights", "Summit", 400, 40),
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
