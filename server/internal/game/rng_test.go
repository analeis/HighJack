package game

import (
	"testing"
)

func TestSeededRngDeterministic(t *testing.T) {
	seed := Seed{1, 2, 3}
	a := NewSeededRng(seed)
	b := NewSeededRng(seed)
	for i := 0; i < 1000; i++ {
		if a.Uint64() != b.Uint64() {
			t.Fatalf("identical seeds diverged at draw %d", i)
		}
	}
}

func TestSeededRngDistinctSeedsDiverge(t *testing.T) {
	a := NewSeededRng(Seed{})
	b := NewSeededRng(Seed{1})
	var same bool
	for i := 0; i < 16; i++ {
		if a.Uint64() == b.Uint64() {
			same = true
			break
		}
	}
	if same {
		t.Fatal("distinct seeds produced identical early output")
	}
}

func TestSplitStreamsAreIndependentAndStable(t *testing.T) {
	root := Seed{42}
	x := Split(root, "cards")
	y := Split(root, "events")
	z := Split(root, "cards")

	if x == y {
		t.Fatal("different labels produced identical child seeds")
	}
	if x != z {
		t.Fatal("same label produced different child seeds")
	}

	ax, bx := NewSeededRng(x), NewSeededRng(x)
	for i := 0; i < 128; i++ {
		if ax.Uint64() != bx.Uint64() {
			t.Fatal("split stream not reproducible")
		}
	}
}

func TestSplitIncorporatesParentSeed(t *testing.T) {
	a := Split(Seed{}, "x")
	b := Split(Seed{1}, "x")
	if a == b {
		t.Fatal("child seed ignores parent seed")
	}
}

func TestIntNBounds(t *testing.T) {
	r := NewSeededRng(Seed{9})
	for i := 1; i <= 50; i++ {
		for j := 0; j < 100; j++ {
			v := r.IntN(i)
			if v < 0 || v >= i {
				t.Fatalf("IntN(%d) out of range: %d", i, v)
			}
		}
	}
}

func TestSeedHexRoundTrip(t *testing.T) {
	s := Seed{0xAB, 0xCD}
	parsed, err := ParseSeed(s.Hex())
	if err != nil {
		t.Fatalf("ParseSeed(%q): %v", s.Hex(), err)
	}
	if parsed != s {
		t.Fatalf("round trip mismatch: %v != %v", parsed, s)
	}
	if _, err := ParseSeed("nothex"); err == nil {
		t.Fatal("expected error for non-hex seed")
	}
	if _, err := ParseSeed("abcd"); err == nil {
		t.Fatal("expected error for short seed")
	}
}

func TestNewSeedIsUnique(t *testing.T) {
	a, err := NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("system entropy returned identical seeds")
	}
}
