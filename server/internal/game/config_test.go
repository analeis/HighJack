package game

import (
	"os"
	"path/filepath"
	"testing"
)

// protocolFixturesDir resolves the shared fixtures owned by
// packages/protocol. Go and TypeScript tests validate against the same
// files, which is what keeps the two wire implementations compatible.
func protocolFixturesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "packages", "protocol", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("protocol fixtures not found at %s", dir)
	}
	return dir
}

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolFixturesDir(t), rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

func TestDefaultConfigIsValidAndMirrorsProtocol(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}

	// The TS DEFAULT_CONFIG is serialized as valid_default.json; decoding it
	// here must produce a structurally identical config.
	wire := readFixture(t, "config/valid_default.json")
	parsed, err := ParseGameConfig(wire)
	if err != nil {
		t.Fatalf("valid_default.json rejected: %v", err)
	}
	if parsed.Version != ConfigSchemaVersion {
		t.Fatalf("fixture version %d != engine schema version %d (bump both sides together)", parsed.Version, ConfigSchemaVersion)
	}
	// GameConfig contains slices and is no longer directly comparable;
	// canonical bytes are the parity contract both sides hash.
	want, err := cfg.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := parsed.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("default configs diverged between Go and TypeScript fixtures:\n got: %s\nwant: %s", got, want)
	}
}

func TestParseGameConfigAcceptsValidFullFixture(t *testing.T) {
	cfg, err := ParseGameConfig(readFixture(t, "config/valid_full.json"))
	if err != nil {
		t.Fatalf("valid_full.json rejected: %v", err)
	}
	if !cfg.Gambling.Poker || !cfg.RandomEvents.Enabled {
		t.Fatal("valid_full fixture did not round-trip its fields")
	}
}

func TestParseGameConfigRejectsMalformedJSONAsStructural(t *testing.T) {
	var verr *ValidationError
	_, err := ParseGameConfig([]byte("{not json"))
	if err == nil {
		t.Fatal("expected error")
	}
	if e, ok := err.(*ValidationError); ok {
		verr = e
	} else {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(verr.Issues) != 1 || verr.Issues[0].Kind != KindStructural {
		t.Fatalf("malformed JSON must be a single structural issue, got %+v", verr.Issues)
	}
}

func TestParseGameConfigRejectsUnknownFields(t *testing.T) {
	doc := []byte(`{"version":1,"mysteryField":true}`)
	if _, err := ParseGameConfig(doc); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestValidateClassifiesStructuralIssues(t *testing.T) {
	var verr *ValidationError
	_, err := ParseGameConfig(readFixture(t, "config/invalid_structural.json"))
	if err == nil {
		t.Fatal("expected structural rejection")
	}
	verr = err.(*ValidationError)
	kinds := map[IssueKind]int{}
	paths := map[string]bool{}
	for _, issue := range verr.Issues {
		kinds[issue.Kind]++
		paths[issue.Path] = true
	}
	if kinds[KindSemantic] != 0 {
		t.Fatalf("structural validation must not emit semantic issues: %+v", verr.Issues)
	}
	for _, want := range []string{"version", "playerCount.min", "playerCount.max", "startingMoney"} {
		if !paths[want] {
			t.Fatalf("expected structural issue at %q, got %+v", want, verr.Issues)
		}
	}
}

func TestValidateClassifiesSemanticIssues(t *testing.T) {
	var verr *ValidationError
	raw := readFixture(t, "config/invalid_semantic.json")

	// Structure alone must pass; semantics must fail — this pins the tier split.
	structuralCfg, err := ParseGameConfig(raw)
	_ = structuralCfg
	if err == nil {
		t.Fatal("expected overall rejection")
	}
	verr = err.(*ValidationError)
	for _, issue := range verr.Issues {
		if issue.Kind != KindSemantic {
			t.Fatalf("expected only semantic issues, got %+v", verr.Issues)
		}
	}
	paths := map[string]bool{}
	for _, issue := range verr.Issues {
		paths[issue.Path] = true
	}
	for _, want := range []string{"playerCount.min", "gambling", "randomEvents.intervalTicks", "victory.roundLimit"} {
		if !paths[want] {
			t.Fatalf("expected semantic issue at %q, got %+v", want, verr.Issues)
		}
	}
}

func TestCanonicalJSONMatchesSharedHashCase(t *testing.T) {
	var testCase struct {
		Input     any    `json:"input"`
		Canonical string `json:"canonical"`
		SHA256    string `json:"sha256"`
	}
	data := readFixture(t, "config/canonical_hash_case.json")
	if err := jsonUnmarshalStrict(data, &testCase); err != nil {
		t.Fatalf("decode hash case: %v", err)
	}

	cfgBytes, err := jsonMarshal(testCase.Input)
	if err != nil {
		t.Fatalf("re-encode hash case input: %v", err)
	}
	cfg, err := ParseGameConfig(cfgBytes)
	if err != nil {
		t.Fatalf("hash case input rejected: %v", err)
	}
	canonical, err := cfg.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != testCase.Canonical {
		t.Fatalf("canonical JSON diverged:\n go:  %s\nwant: %s", canonical, testCase.Canonical)
	}
	hash, err := cfg.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash != testCase.SHA256 {
		t.Fatalf("hash diverged:\n go:  %s\nwant: %s", hash, testCase.SHA256)
	}
}
