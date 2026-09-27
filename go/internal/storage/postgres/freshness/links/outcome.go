// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"errors"
	"fmt"
	"time"
)

// LinkKind names how one activation was linked.
type LinkKind string

const (
	// LinkKindRoot builds a scope's state from its first full generation.
	LinkKindRoot LinkKind = "root"
	// LinkKindIncremental links the state generation to a later full
	// generation in one aggregate of the later generation.
	LinkKindIncremental LinkKind = "incremental"
	// LinkKindNone marks an activation that produced no link: a chain break,
	// or an activation of the generation the state already describes.
	LinkKindNone LinkKind = "none"
)

// BreakReason names why an activation advanced the cursor without a link.
// The state is kept at its generation (#7127 ruling 8.5).
type BreakReason string

const (
	// BreakPrunedBeforeLink means the activation's generation row no longer
	// exists: retention pruned it before the writer reached it.
	BreakPrunedBeforeLink BreakReason = "pruned_before_link"
	// BreakDeltaWithoutRoot means a delta generation activated while the
	// scope had no state; the scope waits for a full generation.
	BreakDeltaWithoutRoot BreakReason = "delta_without_root"
	// BreakPriorMismatch means a delta generation's recorded prior is unknown
	// or is not the state generation, so no overlay could be exact.
	BreakPriorMismatch BreakReason = "prior_mismatch"
	// BreakOverlayUnproven means a delta generation's prior matches the state,
	// but the overlay link is not shipped: its delta-kind ownership proof
	// (#7127 ruling 8.6, gate G2) has not passed.
	BreakOverlayUnproven BreakReason = "overlay_unproven"
	// BreakLinkPoisoned means the activation's link failed with a counting
	// failure MaxAttempts times. RecordFailure advanced the cursor past it,
	// kept the state, and set the scope's poison marker, which the next full
	// link clears (#7127 ruling 8.10).
	BreakLinkPoisoned BreakReason = "link_poisoned"
)

// RetryReason names a non-counting outcome: a non-blocking lock miss. The
// transaction rolled back, nothing was written, the cursor did not move, and
// no attempt is counted (#7127 ruling 8.10). The runner moves on to its next
// candidate and retries this activation on a later cycle.
type RetryReason string

const (
	// RetryCursorLocked means another writer holds the scope's cursor row.
	RetryCursorLocked RetryReason = "cursor_locked"
	// RetryGenerationLocked means another transaction (generation retention)
	// holds the activating generation's row.
	RetryGenerationLocked RetryReason = "generation_locked"
	// RetrySlotBusy means every full-link slot is held.
	RetrySlotBusy RetryReason = "slot_busy"
)

// RetryError reports a retryable link outcome. It satisfies the reducer's
// contract.RetryableError interface (Retryable() bool) without importing the
// reducer, so callers classify it with contract.IsRetryable.
type RetryError struct {
	Reason RetryReason
	// ScopeID and ActivationSeq name the head activation when the miss
	// happened after it was read (generation_locked, slot_busy); zero for
	// cursor_locked.
	ScopeID       string
	ActivationSeq int64
	Err           error
}

// Error describes the retry reason and the underlying cause, if any.
func (e *RetryError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("changed-since link retry (%s): %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("changed-since link retry (%s)", e.Reason)
}

// Unwrap returns the underlying cause.
func (e *RetryError) Unwrap() error { return e.Err }

// Retryable reports true: every RetryError is safe to retry and counts no
// attempt.
func (e *RetryError) Retryable() bool { return true }

// RetryReasonOf returns the reason of a RetryError in err's chain, and false
// when err carries none.
func RetryReasonOf(err error) (RetryReason, bool) {
	var retry *RetryError
	if !errors.As(err, &retry) {
		return "", false
	}
	return retry.Reason, true
}

// LinkResult describes one committed link transaction. Idle is true when the
// scope had no activation above its cursor; nothing else is set then.
type LinkResult struct {
	Idle bool
	// Deferred is set with Idle when the head activation is backing off
	// after a counting failure and its next_attempt_at is still ahead.
	Deferred bool

	ScopeID           string
	GenerationID      string
	PriorGenerationID string
	ActivationSeq     int64
	Kind              LinkKind
	Break             BreakReason

	// DeltaRows is the number of link delta rows written.
	DeltaRows int64
	// Keys is the effective key count of the linked generation, all
	// categories.
	Keys     int64
	Duration time.Duration
}
