// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"strings"
	"testing"
)

// TestAWSDriftFingerprintStatementsSharePairFragment pins that the listing
// scalar and the snapshot rows derive from the same per-ARN pair fragment:
// the read and the reopen can never compute the per-ARN identity two
// different ways (#11018 P2 collapse rule).
func TestAWSDriftFingerprintStatementsSharePairFragment(t *testing.T) {
	t.Parallel()
	pairs := awsDriftARNPairsSQL("work.scope_id", "scope.active_generation_id")
	if !strings.Contains(AWSDriftARNFingerprintSQL("work.scope_id", "scope.active_generation_id"), pairs) {
		t.Fatal("AWSDriftARNFingerprintSQL does not embed the shared pair fragment")
	}
	if !strings.Contains(AWSDriftSnapshotRowsSQL, awsDriftARNPairsSQL("$1", "$2")) {
		t.Fatal("AWSDriftSnapshotRowsSQL does not embed the shared pair fragment")
	}
	// The collapse rule itself: exactly one GROUP BY on the ARN, so one
	// row per ARN reaches both the digest and the record path.
	if got := strings.Count(pairs, "GROUP BY"); got != 1 {
		t.Fatalf("pair fragment GROUP BY count = %d, want 1 (collapse on ARN only)", got)
	}
	if strings.Contains(pairs, "GROUP BY pair.arn,") || strings.Contains(pairs, "GROUP BY fact.payload") {
		t.Fatalf("pair fragment groups by more than the ARN:\n%s", pairs)
	}
}
