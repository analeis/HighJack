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
		}
	case VictoryRoundLimit:
		if c.Victory.RoundLimit <= 0 {
			add(semantic("victory.roundLimit", "round limit must be positive for round_limit victory"))
		}
	}
	return issues
}
