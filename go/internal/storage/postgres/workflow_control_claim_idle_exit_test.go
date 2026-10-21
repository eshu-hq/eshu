// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

// TestClaimDepthQuiescentRequiresQuiescenceProof is the #8725 regression.
//
// A claimer that observes a full budget of consecutive empty
// ClaimNextEligible returns while peers are still granting (the global grant
// sequence advanced during the streak) is seeing head-of-line lock
// contention, not a drained backlog, and must keep polling — not idle.
// Idling there stranded a claimer at 9 grants against the 10-grant floor
// while the rest of the drain still completed exactly once.
func TestClaimDepthQuiescentRequiresQuiescenceProof(t *testing.T) {
	t.Parallel()

	// Below budget: keep polling even with no global progress.
	if claimDepthQuiescent(maxClaimDepthConsecutiveEmpties-1, 41, 41) {
		t.Fatalf("quiescent(%d empties, no progress) = true, want false (budget not reached)",
			maxClaimDepthConsecutiveEmpties-1)
	}

	// Full budget WITH global progress: contention — keep polling. This is
	// the #8725 signature: a full budget of consecutive all-heads-locked
	// empties while peers drained the rest exactly once.
	if claimDepthQuiescent(maxClaimDepthConsecutiveEmpties, 41, 42) {
		t.Fatalf("quiescent(budget reached, seq 41 -> 42) = true, want false (grants landed during the streak)")
	}

	// Full budget with no progress: quiescent — idle.
	if !claimDepthQuiescent(maxClaimDepthConsecutiveEmpties, 41, 41) {
		t.Fatalf("quiescent(budget reached, seq 41 -> 41) = false, want true (no grants during the streak)")
	}

	// Past budget with no progress stays quiescent.
	if !claimDepthQuiescent(maxClaimDepthConsecutiveEmpties+10, 7, 7) {
		t.Fatalf("quiescent(budget+10, no progress) = false, want true")
	}

	// Zero-length streak never idles, even with a settled sequence.
	if claimDepthQuiescent(0, 9, 9) {
		t.Fatalf("quiescent(0 empties) = true, want false")
	}
}
