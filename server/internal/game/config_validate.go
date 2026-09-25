package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// IssueKind distinguishes configuration validation failures so callers can
// report them appropriately:
//
//   - KindStructural: the JSON was well-formed but has wrong shape, types,
//     or out-of-range values. A client bug or hand-edited file.
//   - KindSemantic: structurally valid but logically impossible (e.g.
//     poker enabled while gambling is disabled). Requires judgment to fix.
//
// Syntactic failures (invalid JSON) are reported by ParseGameConfig as a
// KindStructural issue with path "" and are not distinguishable from shape
// errors at the API level; transport layers may pre-parse for a stricter
// split if they need one.
type IssueKind string

const (
	KindStructural IssueKind = "structural"
	KindSemantic   IssueKind = "semantic"
)

// ValidationIssue identifies one config problem with its dotted path
// (e.g. "playerCount.min"), mirroring ConfigIssue in packages/protocol.
type ValidationIssue struct {
	Kind    IssueKind `json:"kind"`
	Path    string    `json:"path"`
	Message string    `json:"message"`
}

func (i ValidationIssue) Error() string {
	return fmt.Sprintf("%s issue at %q: %s", i.Kind, i.Path, i.Message)
}

// ValidationError aggregates all issues found in one configuration.
type ValidationError struct {
	Issues []ValidationIssue `json:"issues"`
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("game config is invalid")
	for _, i := range e.Issues {
		fmt.Fprintf(&b, "\n  - %s", i.Error())
	}
	return b.String()
}

func structural(path, format string, args ...any) ValidationIssue {
	return ValidationIssue{Kind: KindStructural, Path: path, Message: fmt.Sprintf(format, args...)}
}

func semantic(path, format string, args ...any) ValidationIssue {
	return ValidationIssue{Kind: KindSemantic, Path: path, Message: fmt.Sprintf(format, args...)}
}

// ParseGameConfig decodes and fully validates a configuration document.
// It returns either a config or a non-nil *ValidationError; never both.
//
// Tiering:
//   - syntactic:  invalid JSON → one structural issue with empty path
//   - structural: generic shape scan (per-field paths, unknown fields,
//     types, ranges) then strict typed decode
//   - semantic:   logical-combination checks after structure passes
func ParseGameConfig(data []byte) (*GameConfig, error) {
	generic, err := decodeGeneric(data)
	if err != nil {
		return nil, &ValidationError{Issues: []ValidationIssue{
			structural("", "malformed JSON: %v", err),
		}}
	}

	var issues []ValidationIssue
	if shapeIssues := validateGenericShape(generic); len(shapeIssues) > 0 {
		issues = append(issues, shapeIssues...)
	} else {
		// Shape is sound; re-encode the generic value so typed decoding
		// cannot fail on shape — only on genuine type mismatches missed by
		// the scan (defensive; treated as structural).
		reencoded, encErr := json.Marshal(generic)
		if encErr != nil {
			issues = append(issues, structural("", "malformed configuration: %v", encErr))
		} else {
			dec := json.NewDecoder(bytes.NewReader(reencoded))
			var cfg GameConfig
			if decErr := dec.Decode(&cfg); decErr != nil {
				issues = append(issues, structural("", "malformed configuration: %v", decErr))
			} else if semErr := cfg.Validate(); semErr != nil {
				return nil, semErr
			} else {
				return &cfg, nil
			}
		}
	}
	if len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}
	return nil, &ValidationError{Issues: []ValidationIssue{structural("", "unreachable validation state")}}
}

// Validate runs structural then semantic validation over an already-decoded
// config. Structural problems short-circuit: semantics assume valid shape.
func (c *GameConfig) Validate() error {
	var issues []ValidationIssue

	issues = append(issues, c.validateStructural()...)
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	issues = append(issues, c.validateSemantics()...)
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func (c *GameConfig) validateStructural() []ValidationIssue {
	var issues []ValidationIssue
	add := func(i ValidationIssue) { issues = append(issues, i) }

	if c.Version != ConfigSchemaVersion {
		add(structural("version", "must be %d", ConfigSchemaVersion))
	}
	if c.PlayerCount.Min < MinPlayersFloor {
		add(structural("playerCount.min", "must be ≥ %d", MinPlayersFloor))
	}
	if c.PlayerCount.Max > MaxPlayersCeiling {
		add(structural("playerCount.max", "must be ≤ %d", MaxPlayersCeiling))
	}
	if c.StartingMoney <= 0 || c.StartingMoney > StartingMoneyMax {
		add(structural("startingMoney", "must be between 1 and %d", StartingMoneyMax))
	}
	if c.RandomEvents.IntervalTicks < 0 {
		add(structural("randomEvents.intervalTicks", "must not be negative"))
	}
	switch c.Victory.Type {
	case VictoryLastStanding, VictoryTargetWealth, VictoryRoundLimit:
		// recognized
	default:
		add(structural("victory.type", "unknown victory type %q", c.Victory.Type))
	}
	if c.Victory.TargetWealth < 0 {
		add(structural("victory.targetWealth", "must not be negative"))
	}
	if c.Victory.RoundLimit < 0 {
		add(structural("victory.roundLimit", "must not be negative"))
	}
	issues = append(issues, c.validateBoardStructural()...)
	if c.PropertyRules.PassingGoBonus < 0 {
		add(structural("propertyRules.passingGoBonus", "must not be negative"))
	}
	if c.PropertyRules.MaxDoublesStreak < 0 {
		add(structural("propertyRules.maxDoublesStreak", "must not be negative"))
	}
	return issues
}

// validateBoardStructural checks the configured board shape: dimensions,
// stable ordering, known kinds, and per-kind field requirements.
func (c *GameConfig) validateBoardStructural() []ValidationIssue {
	var issues []ValidationIssue
	add := func(i ValidationIssue) { issues = append(issues, i) }

	n := len(c.Board.Spaces)
	if n < MinBoardSpaces || n > MaxBoardSpaces {
		add(structural("board.spaces", "must have between %d and %d spaces", MinBoardSpaces, MaxBoardSpaces))
		return issues
	}
	if c.Board.Spaces[0].Kind != SpaceGo {
		add(structural("board.spaces[0].kind", "first space must be go"))
	}
	seen := make(map[string]bool, n)
	for i, s := range c.Board.Spaces {
		path := fmt.Sprintf("board.spaces[%d]", i)
		if len(s.ID) == 0 || len(s.ID) > MaxSpaceIDLen {
			add(structural(path+".id", "must be 1..%d characters", MaxSpaceIDLen))
		} else if seen[s.ID] {
			add(structural(path+".id", "duplicate space id %q", s.ID))
		} else {
			seen[s.ID] = true
		}
		if len(s.Name) == 0 || len(s.Name) > MaxSpaceName {
			add(structural(path+".name", "must be 1..%d characters", MaxSpaceName))
		}
		switch s.Kind {
		case SpaceGo, SpaceNeutral:
			// no priced fields
		case SpaceProperty:
			if len(s.Group) == 0 || len(s.Group) > MaxGroupLen {
				add(structural(path+".group", "property group must be 1..%d characters", MaxGroupLen))
			}
			if s.Price <= 0 || s.Price > MaxSpacePrice {
				add(structural(path+".price", "must be between 1 and %d", MaxSpacePrice))
			}
			if s.Rent < 0 || s.Rent > MaxSpaceRent {
				add(structural(path+".rent", "must be between 0 and %d", MaxSpaceRent))
			}
		case SpaceTax:
			if s.Amount <= 0 || s.Amount > MaxTaxAmount {
				add(structural(path+".amount", "must be between 1 and %d", MaxTaxAmount))
			}
		default:
			add(structural(path+".kind", "unknown space kind %q", s.Kind))
		}
	}
	return issues
}

func (c *GameConfig) validateSemantics() []ValidationIssue {
	var issues []ValidationIssue
	add := func(i ValidationIssue) { issues = append(issues, i) }

	if c.PlayerCount.Min > c.PlayerCount.Max {
		add(semantic("playerCount.min", "min players exceeds max players"))
	}
	if !c.Gambling.Enabled && (c.Gambling.Poker || c.Gambling.Blackjack || c.Gambling.Casino) {
		add(semantic("gambling", "individual gambling games require gambling.enabled"))
	}
	if c.RandomEvents.Enabled && c.RandomEvents.IntervalTicks <= 0 {
		add(semantic("randomEvents.intervalTicks", "interval must be positive when random events are enabled"))
	}
	switch c.Victory.Type {
	case VictoryTargetWealth:
		if c.Victory.TargetWealth <= 0 {
			add(semantic("victory.targetWealth", "target wealth must be positive for target_wealth victory"))
		} else if Money(c.Victory.TargetWealth) <= c.StartingMoney {
			// Net worth starts at startingMoney with no holdings: a target
			// at or below it would end the match on the first action.
			add(semantic("victory.targetWealth", "target wealth must exceed starting money"))
		}
	case VictoryRoundLimit:
		if c.Victory.RoundLimit <= 0 {
			add(semantic("victory.roundLimit", "round limit must be positive for round_limit victory"))
		}
	}
	if c.PropertyRules.DoublesExtraRoll && c.PropertyRules.MaxDoublesStreak < 1 {
		add(semantic("propertyRules.maxDoublesStreak", "must be at least 1 when doubles grant extra rolls"))
	}
	return issues
}
