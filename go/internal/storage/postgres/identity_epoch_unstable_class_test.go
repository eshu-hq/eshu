// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"
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
