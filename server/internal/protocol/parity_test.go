package protocol_test

// Cross-language vocabulary parity.
//
// The TypeScript package in packages/protocol and the Go mirror in
// server/internal/protocol are two hand-maintained copies of one wire contract.
// Nothing asserted they agreed, which is how the Go side gained an
// `interrupted` match phase while the TypeScript union still listed only three,
// and how an error code could be added to one side alone. These tests read the
// TypeScript source and compare it, element by element, with the Go constants.
//
// Reading source rather than a build artifact is deliberate: it runs in a
// millisecond, needs no toolchain beyond Go, and fails on the exact commit that
// introduced the drift.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/analeis/highjack/server/internal/game"
	"github.com/analeis/highjack/server/internal/protocol"
)

const tsRoot = "../../../packages/protocol/src/"

// stringArray extracts the quoted members of a `as const` string array literal
// from a TypeScript source file, e.g. `export const ERROR_CODES = [...]`.
func stringArray(t *testing.T, file, constName string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(tsRoot, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	src := string(raw)
	start := strings.Index(src, constName)
	if start < 0 {
		t.Fatalf("%s does not declare %s", file, constName)
	}
	open := strings.Index(src[start:], "[")
	if open < 0 {
		t.Fatalf("%s: %s has no array literal", file, constName)
	}
	rest := src[start+open:]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatalf("%s: %s array is unterminated", file, constName)
	}
	body := rest[:end]

	re := regexp.MustCompile(`'([^']*)'|"([^"]*)"|` + "`([^`]*)`")
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: %s yielded no members", file, constName)
	}
	return out
}

// assertSameMembers requires both sides to contain exactly the same set, in any
// order. Order is not part of the contract, membership is.
func assertSameMembers(t *testing.T, what string, ts, goSide []string) {
	t.Helper()
	tsSet := map[string]bool{}
	for _, v := range ts {
		tsSet[v] = true
	}
	goSet := map[string]bool{}
	for _, v := range goSide {
		goSet[v] = true
	}
	var onlyTS, onlyGo []string
	for v := range tsSet {
		if !goSet[v] {
			onlyTS = append(onlyTS, v)
		}
	}
	for v := range goSet {
		if !tsSet[v] {
			onlyGo = append(onlyGo, v)
		}
	}
	sort.Strings(onlyTS)
	sort.Strings(onlyGo)
	if len(onlyTS) > 0 || len(onlyGo) > 0 {
		t.Fatalf("%s drifted between TypeScript and Go:\n  only in TS: %v\n  only in Go: %v",
			what, onlyTS, onlyGo)
	}
}

func TestErrorCodesMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "errors.ts", "ERROR_CODES")
	var goCodes []string
	for _, c := range protocol.AllErrorCodes() {
		goCodes = append(goCodes, string(c))
	}
	assertSameMembers(t, "ERROR_CODES", ts, goCodes)
}

func TestActionTypesMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "actions.ts", "ACTION_TYPES")
	var goTypes []string
	for _, a := range game.AllActionTypes() {
		goTypes = append(goTypes, string(a))
	}
	assertSameMembers(t, "ACTION_TYPES", ts, goTypes)
}

func TestEventTypesMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "events.ts", "EVENT_TYPES")
	var goTypes []string
	for _, e := range game.AllEventTypes() {
		goTypes = append(goTypes, string(e))
	}
	assertSameMembers(t, "EVENT_TYPES", ts, goTypes)
}

// Match phases are the check that would have caught the interrupted-phase gap:
// the Go engine gained a fourth terminal phase that the TypeScript union did not
// declare, so an authoritative snapshot could fail its own type guard.
func TestMatchPhasesMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "state.ts", "MATCH_PHASES")
	var goPhases []string
	for _, p := range game.AllPhases() {
		goPhases = append(goPhases, string(p))
	}
	assertSameMembers(t, "MATCH_PHASES", ts, goPhases)
}

func TestTurnPhasesMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "state.ts", "TURN_PHASES")
	var goPhases []string
	for _, p := range game.AllTurnPhases() {
		goPhases = append(goPhases, string(p))
	}
	assertSameMembers(t, "TURN_PHASES", ts, goPhases)
}

func TestSpaceKindsMatchTypeScript(t *testing.T) {
	ts := stringArray(t, "config.ts", "SPACE_KINDS")
	var goKinds []string
	for _, k := range game.AllSpaceKinds() {
		goKinds = append(goKinds, string(k))
	}
	assertSameMembers(t, "SPACE_KINDS", ts, goKinds)
}

// The protocol version is asserted, not assumed: a drift here silently changes
// what clients are told the contract is.
func TestProtocolVersionMatchesTypeScript(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(tsRoot, "version.ts"))
	if err != nil {
		t.Fatalf("read version.ts: %v", err)
	}
	re := regexp.MustCompile(`PROTOCOL_VERSION\s*=\s*'([^']+)'`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("version.ts does not declare PROTOCOL_VERSION as a string literal")
	}
	if m[1] != protocol.ProtocolVersion {
		t.Fatalf("protocol version drift: TypeScript %q, Go %q", m[1], protocol.ProtocolVersion)
	}
	if m[1][:1] != "1" {
		t.Fatalf("protocol major changed to %q; that is a wire-compatibility break", m[1])
	}
}
