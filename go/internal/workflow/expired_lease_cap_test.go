// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "testing"

// TestDefaultExpiredLeaseMaxRequeuesPinsValue pins the expired-lease requeue
// cap next to the attempt budget (eshu-hq/eshu#10113). The value is
// intentionally equal to DefaultClaimMaxAttempts so one mental model bounds
// both a run that keeps returning retryable failures and a run that never
// returns at all.
func TestDefaultExpiredLeaseMaxRequeuesPinsValue(t *testing.T) {
	t.Parallel()

	if got, want := DefaultExpiredLeaseMaxRequeues(), 10; got != want {
		t.Fatalf("DefaultExpiredLeaseMaxRequeues() = %d, want %d", got, want)
	}
	if got, want := DefaultClaimMaxAttempts(), 10; got != want {
		t.Fatalf("DefaultClaimMaxAttempts() = %d, want %d", got, want)
	}
}

// TestExpiredLeaseBudgetExhaustedClassPinsValue pins the named dead-letter
// class a work item carries when its lease expires past the cap without a
// returned failure (eshu-hq/eshu#10113).
func TestExpiredLeaseBudgetExhaustedClassPinsValue(t *testing.T) {
	t.Parallel()

	if got, want := FailureClassExpiredLeaseBudgetExhausted, "expired_lease_budget_exhausted"; got != want {
		t.Fatalf("FailureClassExpiredLeaseBudgetExhausted = %q, want %q", got, want)
	}
}
