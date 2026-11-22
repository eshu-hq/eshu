// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// TestUnsafeReplayFailureClassesPinnedToSourceConstants keeps the one
// unsafe-replay set bound to the constants it is built from: the projector's
// non-retryable input class, the semantic queue quarantine, and every
// manual-review triage class.
func TestUnsafeReplayFailureClassesPinnedToSourceConstants(t *testing.T) {
	t.Parallel()
	want := []string{string(failure.FailureClassInputInvalid), string(semanticqueue.StatusUnsafePayload)}
	want = append(want, failure.ManualReviewTriageClasses()...)
	guidance := UnsafeReplayFailureClassGuidance()
	for _, class := range want {
		if guidance[class] == "" {
			t.Fatalf("class %q missing from the unsafe-replay set or has no guidance", class)
		}
	}
	got := UnsafeReplayFailureClasses()
	if len(got) != len(guidance) {
		t.Fatalf("list has %d classes, guidance map has %d", len(got), len(guidance))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("list %v is not sorted", got)
	}
	for _, retryable := range []string{"supply_chain_impact_fanout_failed", "reducer_handler_timeout", "dependency_unavailable"} {
		if _, unsafe := guidance[retryable]; unsafe {
			t.Fatalf("retryable class %q must not be unsafe", retryable)
		}
	}
}

// TestUnsafeReplayFailureClassGuidanceReturnsACopy proves a caller that edits
// its copy cannot change the shared set.
func TestUnsafeReplayFailureClassGuidanceReturnsACopy(t *testing.T) {
	t.Parallel()
	first := UnsafeReplayFailureClassGuidance()
	first["planted"] = "x"
	if _, leaked := UnsafeReplayFailureClassGuidance()["planted"]; leaked {
		t.Fatal("UnsafeReplayFailureClassGuidance() returned the shared map")
	}
}
