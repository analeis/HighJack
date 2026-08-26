package game

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Generic shape validation mirrors validateStructure in
// packages/protocol/src/config.ts so both sides report equivalent issues
// with identical dotted paths. It runs on a generic decode of the raw JSON
// before typed decoding, which lets type errors be attributed to precise
// fields instead of collapsing into one unmarshal failure.
func validateGenericShape(root any) []ValidationIssue {
	obj, ok := root.(map[string]any)
	if !ok {
		return []ValidationIssue{structural("", "config must be an object")}
	}
	var issues []ValidationIssue
	add := func(path, format string, args ...any) {
		issues = append(issues, structural(path, format, args...))
	}

	known := map[string]bool{
		"version": true, "playerCount": true, "startingMoney": true,
		"trading": true, "auctions": true, "gambling": true,
		"carnival": true, "sports": true, "cards": true,
		"randomEvents": true, "victory": true,
	}
	for key := range obj {
		if !known[key] {
			add(key, "unknown field %q", key)
		}
	}

	// version
	switch v := obj["version"].(type) {
	case json.Number:
		if v.String() != fmt.Sprint(ConfigSchemaVersion) {
			add("version", "must be %d", ConfigSchemaVersion)
		}
	default:
		add("version", "must be an integer")
	}

	// playerCount
	pc, present := obj["playerCount"].(map[string]any)
	if !present {
		add("playerCount", "must be an object")
	} else {
		min, minOK := pc["min"].(json.Number)
		max, maxOK := pc["max"].(json.Number)
		if !minOK {
			add("playerCount.min", "must be an integer")
		} else if n, err := min.Int64(); err != nil || int(n) < MinPlayersFloor {
			add("playerCount.min", "must be ≥ %d", MinPlayersFloor)
		}
		if !maxOK {
			add("playerCount.max", "must be an integer")
		} else if n, err := max.Int64(); err != nil || int(n) > MaxPlayersCeiling {
			add("playerCount.max", "must be ≤ %d", MaxPlayersCeiling)
		}
		for key := range pc {
			if key != "min" && key != "max" {
				add("playerCount."+key, "unknown field %q", key)
			}
		}
	}

	// startingMoney
	switch sm := obj["startingMoney"].(type) {
	case json.Number:
		n, err := sm.Int64()
		if err != nil || n <= 0 || n > StartingMoneyMax {
			add("startingMoney", "must be between 1 and %d", StartingMoneyMax)
		}
	default:
		add("startingMoney", "must be an integer")
	}

	// simple toggles
	for _, name := range []string{"trading", "auctions", "carnival", "sports", "cards"} {
		toggleObj, present := obj[name].(map[string]any)
		if !present {
			add(name, "must be an object")
			continue
		}
		enabled, enabledPresent := toggleObj["enabled"]
		switch enabled.(type) {
		case bool:
			// ok
		default:
			_ = enabled
			if !enabledPresent {
				issues = append(issues, ValidationIssue{Kind: KindStructural, Path: name + ".enabled", Message: "missing required field"})
			} else {
				add(name+".enabled", "must be a boolean")
			}
		}
		for key := range toggleObj {
			if key != "enabled" {
				add(name+"."+key, "unknown field %q", key)
			}
		}
	}

	// gambling
	gamb, present := obj["gambling"].(map[string]any)
	if !present {
		add("gambling", "must be an object")
	} else {
		for _, flag := range []string{"enabled", "poker", "blackjack", "casino"} {
			if v, exists := gamb[flag]; exists {
				if _, isBool := v.(bool); !isBool {
					add("gambling."+flag, "must be a boolean")
				}
			} else {
				issues = append(issues, ValidationIssue{Kind: KindStructural, Path: "gambling." + flag, Message: "missing required field"})
			}
		}
		for key := range gamb {
			if key != "enabled" && key != "poker" && key != "blackjack" && key != "casino" {
				add("gambling."+key, "unknown field %q", key)
			}
		}
	}

	// randomEvents
	re, present := obj["randomEvents"].(map[string]any)
	if !present {
		add("randomEvents", "must be an object")
	} else {
		if v, exists := re["enabled"]; exists {
			if _, isBool := v.(bool); !isBool {
				add("randomEvents.enabled", "must be a boolean")
			}
		} else {
			issues = append(issues, ValidationIssue{Kind: KindStructural, Path: "randomEvents.enabled", Message: "missing required field"})
		}
		switch interval := re["intervalTicks"].(type) {
		case json.Number:
			if n, err := interval.Int64(); err != nil || n < 0 {
				add("randomEvents.intervalTicks", "must not be negative")
			}
		default:
			add("randomEvents.intervalTicks", "must be an integer")
		}
		for key := range re {
			if key != "enabled" && key != "intervalTicks" {
				add("randomEvents."+key, "unknown field %q", key)
			}
		}
	}

	// victory
	vic, present := obj["victory"].(map[string]any)
	if !present {
		add("victory", "must be an object")
	} else {
		switch vt := vic["type"].(type) {
		case string:
			switch VictoryType(vt) {
			case VictoryLastStanding, VictoryTargetWealth, VictoryRoundLimit:
			default:
				add("victory.type", "unknown victory type %q", vt)
			}
		default:
			add("victory.type", "must be a string")
		}
		switch tw := vic["targetWealth"].(type) {
		case json.Number:
			if n, err := tw.Int64(); err != nil || n < 0 {
				add("victory.targetWealth", "must not be negative")
			}
		default:
			add("victory.targetWealth", "must be an integer")
		}
		switch rl := vic["roundLimit"].(type) {
		case json.Number:
			if n, err := rl.Int64(); err != nil || n < 0 {
				add("victory.roundLimit", "must not be negative")
			}
		default:
			add("victory.roundLimit", "must be an integer")
		}
		for key := range vic {
			if key != "type" && key != "targetWealth" && key != "roundLimit" {
				add("victory."+key, "unknown field %q", key)
			}
		}
	}

	return issues
}

func decodeGeneric(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return generic, nil
}
