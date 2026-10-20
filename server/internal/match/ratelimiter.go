// This file holds the per-session rate limiter.
//
// It is a process-global abuse guard, not gameplay logic, and it is bounded: the
// map of tracked keys is swept as it goes, so a connection that has gone away
// does not leave an entry behind for the life of the process.

package match

import (
	"sync"
	"time"
)

// RateLimiter bounds actions per session on a sliding window. It is a
// per-connection abuse guard, not gameplay logic.
type RateLimiter struct {
	swept  int
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

// NewRateLimiter builds a limiter allowing `limit` events per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time), now: time.Now}
}

// Allow reports whether key may act now, recording the attempt when it may.
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-r.window)

	// Amortised sweep, before any early return. Only the queried key used to be
	// pruned, and the map is keyed by session ids that are never reused, so every
	// connection ever opened left a key behind for the life of the process —
	// roughly 560 bytes each, hundreds of megabytes a day at a plausible
	// connection rate, reachable by nothing. It runs first because a flood of
	// denied requests returns early, and that is exactly when the map would
	// otherwise stop shrinking.
	r.swept++
	if r.swept >= sweepEvery {
		r.swept = 0
		for k, times := range r.hits {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(r.hits, k)
			}
		}
	}

	kept := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}
	r.hits[key] = append(kept, now)
	return true
}

// tracked reports how many keys the limiter is holding. Exposed for the eviction
// test; production code has no reason to ask.
func (r *RateLimiter) tracked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hits)
}

// sweepEvery is the number of Allow calls between eviction sweeps. Amortised so
// the cost is O(1) per call rather than O(keys) on every call.
const sweepEvery = 64

// reconcileCommit resolves a commit whose outcome is unknown and returns the
// error the caller should report.
//
// A commit that returns an error did not necessarily fail. The 5s deadline can
// expire while COMMIT is in flight, and the connection can drop after the server
// made the transaction durable but before the result reached us. Treating that as
// "did not happen" and reverting memory is what created a permanent divergence:
// memory sat one tick behind the database, the next transition's INSERT then
// collided on the (match_id, tick, seq) primary key, and every later action failed
// the same way, bricking the match.
//
// So the durable cursor is read back. If it moved, the transition committed and
// memory is corrected to agree; if it did not, memory is restored. Either way the
// caller is told the truth.
//
// Called with m.mu held, like the rest of ApplyAction. The read-back is a network
// round trip, so it is bounded by reconcileTimeout and happens on a caller that
// is already serialised per match; the alternative — releasing the lock and
// re-taking it — would let two transitions interleave around the ambiguity, which
// is a far worse failure than a bounded stall.
