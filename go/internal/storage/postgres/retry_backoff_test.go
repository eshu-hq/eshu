// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestComputeRetryDelayAppliesExponentialBackoff proves the base delay
// doubles per attempt (issue #4450: visible_at = now + baseDelay*(1<<attempt)
// + jitter) instead of staying fixed at baseDelay regardless of attempt
// count, which is what let many simultaneously-failing items reconverge on
// the same instant.
func TestComputeRetryDelayAppliesExponentialBackoff(t *testing.T) {
	t.Parallel()

	baseDelay := 30 * time.Second
	maxDelay := time.Hour
	zeroJitter := func() float64 { return 0 }

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 30 * time.Second},
		{attempt: 1, want: 60 * time.Second},
		{attempt: 2, want: 120 * time.Second},
		{attempt: 3, want: 240 * time.Second},
	}
	for _, tc := range cases {
		got := computeRetryDelay(baseDelay, maxDelay, 0.1, tc.attempt, zeroJitter)
		if got != tc.want {
			t.Fatalf("computeRetryDelay(attempt=%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

// TestComputeRetryDelayCapsAtMaxDelay proves the exponential term is bounded
// so a high attempt count (still within MaxAttempts for callers with a large
// budget, or a defensive cap for any caller) cannot overflow into an
// unreasonable delay.
func TestComputeRetryDelayCapsAtMaxDelay(t *testing.T) {
	t.Parallel()

	baseDelay := 30 * time.Second
	maxDelay := 5 * time.Minute
	zeroJitter := func() float64 { return 0 }

	got := computeRetryDelay(baseDelay, maxDelay, 0.1, 10, zeroJitter)
	if got != maxDelay {
		t.Fatalf("computeRetryDelay(attempt=10) = %v, want capped %v", got, maxDelay)
	}
}

// TestComputeRetryDelayCapsAtMaxDelayWithoutOverflowingLargeAttemptCounts
// proves computeRetryDelay stays capped at maxDelay (never wraps to a
// negative duration) for an attempt count large enough that a naive
// baseDelay*(1<<attempt) computation overflows time.Duration's int64
// nanosecond range. This is the exact shape of a non-counting reducer
// readiness-class failure (e.g. a kubernetes-correlation or secrets/IAM
// readiness miss) whose AttemptCount grows unbounded because it never
// reaches the dead-letter path: a multi-minute baseDelay at attempt=42
// overflows the naive shift-then-cap sequence into a negative result,
// which would schedule visible_at in the past instead of respecting
// maxDelay — worse than the pre-#4450 fixed-delay behavior it replaced.
func TestComputeRetryDelayCapsAtMaxDelayWithoutOverflowingLargeAttemptCounts(t *testing.T) {
	t.Parallel()

	baseDelay := 2 * time.Minute
	maxDelay := time.Hour
	zeroJitter := func() float64 { return 0 }

	got := computeRetryDelay(baseDelay, maxDelay, 0.1, 42, zeroJitter)
	if got != maxDelay {
		t.Fatalf("computeRetryDelay(attempt=42) = %v, want capped %v (non-negative)", got, maxDelay)
	}
	if got < 0 {
		t.Fatalf("computeRetryDelay(attempt=42) = %v, want non-negative", got)
	}

	// Also probe exactly at maxBackoffShift, the loop-iteration bound, to
	// prove the doubling loop's own overflow break (doubled < backoff) holds
	// even at the clamp boundary.
	got = computeRetryDelay(baseDelay, maxDelay, 0.1, maxBackoffShift, zeroJitter)
	if got != maxDelay {
		t.Fatalf("computeRetryDelay(attempt=maxBackoffShift) = %v, want capped %v (non-negative)", got, maxDelay)
	}
}

// TestComputeRetryDelayAddsBoundedJitter proves jitter is added on top of the
// exponential base and stays within [0, baseDelay*jitterFraction) for the
// pre-cap delay, per the issue formula rand(0, baseDelay*0.1).
func TestComputeRetryDelayAddsBoundedJitter(t *testing.T) {
	t.Parallel()

	baseDelay := 30 * time.Second
	maxDelay := time.Hour
	jitterFraction := 0.1

	full := func() float64 { return 1.0 }
	got := computeRetryDelay(baseDelay, maxDelay, jitterFraction, 0, full)
	want := baseDelay + time.Duration(float64(baseDelay)*jitterFraction)
	if got != want {
		t.Fatalf("computeRetryDelay with max jitter = %v, want %v", got, want)
	}

	none := func() float64 { return 0.0 }
	got = computeRetryDelay(baseDelay, maxDelay, jitterFraction, 0, none)
	if got != baseDelay {
		t.Fatalf("computeRetryDelay with zero jitter = %v, want %v", got, baseDelay)
	}
}

// TestComputeRetryDelayNegativeAttemptTreatedAsZero guards against a caller
// passing a pre-increment attempt count of -1 or similar; the function must
// not panic or produce a negative shift.
func TestComputeRetryDelayNegativeAttemptTreatedAsZero(t *testing.T) {
	t.Parallel()

	baseDelay := 30 * time.Second
	maxDelay := time.Hour
	zeroJitter := func() float64 { return 0 }

	got := computeRetryDelay(baseDelay, maxDelay, 0.1, -5, zeroJitter)
	if got != baseDelay {
		t.Fatalf("computeRetryDelay(attempt=-5) = %v, want %v (treated as attempt=0)", got, baseDelay)
	}
}

// TestRetrySurgeSpreadsVisibleAtAcrossManySimultaneousFailures is the
// two-sided proof the issue asks for: 100 items that all fail at the exact
// same instant with the exact same attempt count must NOT reconverge on one
// visible_at. Without jitter (fraction=0) they are byte-identical, which is
// the retry-storm bug; with jitter enabled and a seeded per-call rand source
// they spread across a bounded window deterministically.
func TestRetrySurgeSpreadsVisibleAtAcrossManySimultaneousFailures(t *testing.T) {
	t.Parallel()

	baseDelay := 30 * time.Second
	maxDelay := time.Hour
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)

	// Deterministic seeded PRNG so the test is reproducible, not flaky.
	seeded := newSeededJitterSource(42)

	visibleAtSet := make(map[time.Time]bool, 100)
	for i := 0; i < 100; i++ {
		delay := computeRetryDelay(baseDelay, maxDelay, 0.1, 0, seeded)
		visibleAtSet[now.Add(delay)] = true
	}

	if got, want := len(visibleAtSet), 100; got != want {
		t.Fatalf("distinct visible_at count = %d, want %d (retry storm: items reconverged)", got, want)
	}

	// Without jitter, prove the storm actually reproduces: all 100 collapse
	// to exactly 1 distinct value.
	noJitter := func() float64 { return 0 }
	collapsed := make(map[time.Time]bool, 100)
	for i := 0; i < 100; i++ {
		delay := computeRetryDelay(baseDelay, maxDelay, 0.1, 0, noJitter)
		collapsed[now.Add(delay)] = true
	}
	if got, want := len(collapsed), 1; got != want {
		t.Fatalf("distinct visible_at count without jitter = %d, want %d", got, want)
	}
}
