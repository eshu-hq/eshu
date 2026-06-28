// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clock

import (
	"fmt"
	"sync"
	"time"
)

// Clock is the injectable time source for the reducer queue/lease/reap path.
//
// Production wiring injects System(); deterministic replay (Layer 3 of the
// deterministic replay framework, epic #4102) and tests inject a Simulated
// clock whose Advance/Set methods move time forward without waiting on the wall
// clock, so lease expiry, claim visibility, and retry-backoff timing can be
// exercised deterministically. Callers that persist a value or compare it
// against a stored deadline normalize to UTC at the use site, matching the
// existing per-struct now() helpers (for example ReducerQueue.now()).
type Clock interface {
	// Now returns the current time. The result is not pre-normalized to UTC;
	// call sites apply .UTC() when persisting or comparing, mirroring the
	// established now() helper convention across the storage layer.
	Now() time.Time
}

// NowFunc adapts a func() time.Time into a Clock, mirroring http.HandlerFunc.
// It lets the codebase-wide `Now func() time.Time` struct seam accept a Clock
// without changing that field convention: a struct exposes a Now func field and
// the composition root assigns clk.Now (a method value) to it.
type NowFunc func() time.Time

// Now calls f.
func (f NowFunc) Now() time.Time { return f() }

// systemClock reads the real wall clock.
type systemClock struct{}

// Now returns the real wall-clock time.
func (systemClock) Now() time.Time { return time.Now() }

// System returns the real wall-clock Clock used in production. It is the
// behavior-preserving default: System().Now() is exactly time.Now(), so wiring
// it where a nil seam previously fell back to time.Now() changes no behavior.
func System() Clock { return systemClock{} }

// Simulated is a controllable Clock for deterministic tests and replay. Its
// methods are safe for concurrent use, so one Simulated can back several
// queue/lease components at once and a single Advance/Set moves the time every
// component observes — the property replay needs to drive lease expiry across
// the whole path from one place.
type Simulated struct {
	mu  sync.Mutex
	now time.Time
}

// NewSimulated returns a Simulated clock anchored at start.
func NewSimulated(start time.Time) *Simulated {
	return &Simulated{now: start}
}

// Now returns the current simulated time.
func (s *Simulated) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// Advance moves the simulated clock forward by d. A zero duration is a no-op.
// A negative duration panics: simulated time must never move backward, because
// lease-expiry and retry deadlines assume a monotonic clock and a backward jump
// would silently mask an expired lease as still held.
func (s *Simulated) Advance(d time.Duration) {
	if d == 0 {
		return
	}
	if d < 0 {
		panic(fmt.Sprintf("clock: Simulated.Advance must not move time backward (d=%s)", d))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = s.now.Add(d)
}

// Set moves the simulated clock to t. It panics if t is before the current
// simulated time, preserving the monotonic-time invariant Advance also enforces.
func (s *Simulated) Set(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Before(s.now) {
		panic(fmt.Sprintf("clock: Simulated.Set must not move time backward (have=%s, want=%s)", s.now, t))
	}
	s.now = t
}
