// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"

	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// TestIdentityEpochUnstableFailureClassCountsClaimAttempts pins the #7805 retry
// bound. A leader whose load stayed torn fails with a retryable error of class
// identity_epoch_unstable. That class must NOT be a non-counting retry class,
// because a non-counting class freezes attempt_count and a persistently moving
// epoch would then retry forever. Counting, it is bounded by the existing
// claim-attempt limit and dead-letter policy.
func TestIdentityEpochUnstableFailureClassCountsClaimAttempts(t *testing.T) {
	t.Parallel()

	if got := IdentityEpochUnstableFailureClass; got != "identity_epoch_unstable" {
		t.Fatalf("IdentityEpochUnstableFailureClass = %q, want identity_epoch_unstable", got)
	}
	if slices.Contains(nonCountingReducerRetryFailureClasses, IdentityEpochUnstableFailureClass) {
		t.Fatalf("%s is registered as a non-counting retry class; its retries would not consume claim attempts and would not be bounded", IdentityEpochUnstableFailureClass)
	}
	err := newIdentityLoadUnstableError(identityDiscardEpochMoved)
	if r, ok := err.(interface{ Retryable() bool }); !ok || !r.Retryable() {
		t.Fatalf("%v is not retryable", err)
	}
}

// TestIdentityEpochUnstableClassSurvivesDeadLetter proves the operator can find
// a repeatedly torn item by its class: a self-classifying error keeps its own
// failure_class when the queue dead-letters it, so the stored class, the
// eshu_dp_queue_dead_letters_total label, and the retry counter label all read
// identity_epoch_unstable (#7805).
func TestIdentityEpochUnstableClassSurvivesDeadLetter(t *testing.T) {
	t.Parallel()

	err := newIdentityLoadUnstableError(identityDiscardEpochMoved)
	class, _, _ := queuestore.DeadLetterTriageMetadata(err, "reduce_intent", true)
	if class != IdentityEpochUnstableFailureClass {
		t.Fatalf("dead-letter failure_class = %q, want %q", class, IdentityEpochUnstableFailureClass)
	}
	if got := queuestore.BoundedFailureClassLabel(class); got != IdentityEpochUnstableFailureClass {
		t.Fatalf("metric label = %q, want the class unchanged", got)
	}
}
