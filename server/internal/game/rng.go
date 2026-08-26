package game

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
)

// Rng is the only source of randomness allowed inside engine logic.
//
// Domain code must never call math/rand or crypto/rand directly; it
// receives an Rng. This keeps every simulation reproducible: same input
// state + same action + same seed ⇒ same resulting state and events,
// which is what makes deterministic tests, replays, and debugging possible.
type Rng interface {
	// Uint64 returns the next uniform value in [0, 1<<64).
	Uint64() uint64
	// IntN returns a uniform value in [0, n). Panics if n <= 0.
	IntN(n int) int
}

// Seed is a 256-bit simulation seed.
type Seed [32]byte

func (s Seed) Hex() string { return hex.EncodeToString(s[:]) }

func (s Seed) String() string { return s.Hex() }

// NewSeed reads entropy from the operating system. Use it at match-creation
// boundaries only; everything downstream must derive from an explicit Seed
// so simulations stay reproducible.
func NewSeed() (Seed, error) {
	var s Seed
	if _, err := cryptorand.Read(s[:]); err != nil {
		return Seed{}, fmt.Errorf("read system randomness: %w", err)
	}
	return s, nil
}

// ParseSeed decodes a hex-encoded seed (64 characters).
func ParseSeed(hexed string) (Seed, error) {
	raw, err := hex.DecodeString(hexed)
	if err != nil {
		return Seed{}, fmt.Errorf("seed is not valid hex: %w", err)
	}
	if len(raw) != len(Seed{}) {
		return Seed{}, fmt.Errorf("seed must be exactly %d bytes", len(Seed{}))
	}
	var s Seed
	copy(s[:], raw)
	return s, nil
}

// SeededRng is a deterministic Rng built on ChaCha8 (math/rand/v2).
// Identical seeds produce identical streams on every platform and run.
type SeededRng struct {
	src *rand.ChaCha8
}

var _ Rng = (*SeededRng)(nil)

// NewSeededRng returns a deterministic random stream for the given seed.
func NewSeededRng(seed Seed) *SeededRng {
	return &SeededRng{src: rand.NewChaCha8(seed)}
}

func (r *SeededRng) Uint64() uint64 {
	var buf [8]byte
	_, _ = r.src.Read(buf[:]) // ChaCha8.Read never fails
	return leUint64(buf)
}

func (r *SeededRng) IntN(n int) int {
	if n <= 0 {
		panic("game: Rng.IntN called with n <= 0")
	}
	return int(r.Uint64() % uint64(n)) // n is small in practice; modulo bias negligible for game use, revisit for gambling-grade fairness
}

func leUint64(b [8]byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// Split derives an independent child stream deterministically from a parent
// seed and a label. Subsystems (e.g. future card shuffles vs event rolls)
// each get their own labeled stream so consuming randomness from one never
// perturbs another's sequence.
func Split(parent Seed, label string) Seed {
	h := sha256.New()
	h.Write(parent[:])
	h.Write([]byte{0})
	h.Write([]byte(label))
	var child Seed
	copy(child[:], h.Sum(nil))
	return child
}

// DeriveMatchSeed produces the per-match seed recorded in GameStartedEvent
// from the engine's root stream. It exists so that replay verification can
// recompute the exact match seed from a known root seed and tick.
func DeriveMatchSeed(root Seed, tick uint64) Seed {
	return Split(root, fmt.Sprintf("match@%d", tick))
}
