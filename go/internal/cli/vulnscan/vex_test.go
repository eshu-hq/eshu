// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"slices"
	"testing"
)

// TestVEXPolicyMarksSkippedReadiness is the #9612 P2 regression: a skipped
// scan establishes no statement, so the machine-readable policy must name
// target_skipped next to the other non-statement readiness states.
func TestVEXPolicyMarksSkippedReadiness(t *testing.T) {
	t.Parallel()
	if !slices.Contains(vexPolicy().NonStatementReadiness, "target_skipped") {
		t.Fatalf("vexPolicy().NonStatementReadiness = %#v, want target_skipped listed",
			vexPolicy().NonStatementReadiness)
	}
}
