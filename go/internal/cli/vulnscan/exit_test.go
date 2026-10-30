// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import "testing"

// TestExitClassificationTargetSkipped is the #9612 P2 regression: a skipped
// scan exits 4 with the target_skipped reason so automation can distinguish a
// planner skip from a failed lookup, which the default arm mislabels.
func TestExitClassificationTargetSkipped(t *testing.T) {
	t.Parallel()
	code, reason := ExitClassification("target_skipped", 0)
	if code != 4 {
		t.Fatalf("ExitClassification(target_skipped, 0) code = %d, want 4", code)
	}
	if reason != "target_skipped" {
		t.Fatalf("ExitClassification(target_skipped, 0) reason = %q, want %q", reason, "target_skipped")
	}
}
