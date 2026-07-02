// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"math/rand/v2"
	"time"
)

// defaultRetryMaxDelayFallback caps the exponential backoff term when a
// caller (ProjectorQueue or ReducerQueue) leaves MaxRetryDelay unset. Matches
// runtime.RetryPolicyConfig's default so a queue constructed directly
// without going through the env-driven config loader still gets a sane cap.
const defaultRetryMaxDelayFallback = time.Hour

// maxBackoffShift bounds the exponentiation shift so a very large attempt
// count (or a caller-misconfigured MaxAttempts) cannot produce a nonsensical
// shift width. This alone does NOT prevent time.Duration overflow: with a
// multi-minute baseDelay, baseDelay*(1<<32) overflows int64 nanoseconds and
// wraps to a negative duration that then fails the "backoff > maxDelay" cap
// check (a negative number is never greater than a positive maxDelay),
// producing a visible_at in the past instead of the intended 1-hour cap. The
// doubling loop below is the actual overflow guard; this constant only
// bounds how many loop iterations are possible.
const maxBackoffShift = 32

// computeRetryDelay returns the exponential-backoff-with-jitter delay to add
// to "now" when scheduling a retry, replacing the historical fixed-delay
// behavior (ProjectorQueue.Fail and ReducerQueue.failIntent previously both
// used now().Add(retryDelay) unconditionally) that let many
// simultaneously-failing work items reconverge on the exact same visible_at
// and self-reinforce into a retry storm that starves new work (#4450).
//
// jitterFraction scales the random component relative to baseDelay: with the
// default 0.1 the jitter is drawn uniformly from [0, baseDelay*0.1). attempt
// is the durable AttemptCount already consumed before this failure; it is
// clamped to 0 for negative inputs and to maxBackoffShift as a loop-iteration
// bound. The exponential term is built by doubling baseDelay one attempt at a
// time and stopping as soon as the running total would meet or exceed
// maxDelay (or would overflow time.Duration), so a very large or
// caller-misconfigured attempt count against a large baseDelay can never
// overflow into a negative duration that would defeat the cap instead of
// being bounded by it. jitterSource must return a value in [0, 1); production
// callers pass a func wrapping math/rand/v2.Float64, and tests pass a seeded
// or fixed source so the distribution is deterministic and reproducible
// rather than flaky.
func computeRetryDelay(
	baseDelay time.Duration,
	maxDelay time.Duration,
	jitterFraction float64,
	attempt int,
	jitterSource func() float64,
) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > maxBackoffShift {
		attempt = maxBackoffShift
	}

	backoff := baseDelay
	for i := 0; i < attempt; i++ {
		if maxDelay > 0 && backoff >= maxDelay {
			break
		}
		doubled := backoff * 2
		if doubled < backoff {
			// Overflowed time.Duration's int64 range: the pre-doubling value
			// already exceeds any sane cap, so stop here and let the
			// maxDelay clamp below do its job.
			break
		}
		backoff = doubled
	}
	if maxDelay > 0 && backoff > maxDelay {
		backoff = maxDelay
	}

	if jitterFraction > 0 {
		jitter := time.Duration(float64(baseDelay) * jitterFraction * jitterSource())
		backoff += jitter
	}

	if maxDelay > 0 && backoff > maxDelay {
		backoff = maxDelay
	}

	return backoff
}

// newSeededJitterSource returns a jitter source function backed by a
// deterministic, seeded PRNG. Production code paths use
// defaultJitterSource (math/rand/v2's global source) instead; this
// constructor exists so tests can assert a reproducible, non-flaky
// distribution over many simulated retries without depending on wall-clock
// entropy.
func newSeededJitterSource(seed uint64) func() float64 {
	source := rand.New(rand.NewPCG(seed, seed)) // #nosec G404 -- deterministic test-only jitter source
	return source.Float64
}

// defaultJitterSource draws from math/rand/v2's global source, matching the
// non-cryptographic jitter pattern already used by
// storage/cypher.RetryingExecutor for graph-write retry backoff.
func defaultJitterSource() float64 {
	return rand.Float64() // #nosec G404 -- non-security jitter for exponential backoff retry delay
}
