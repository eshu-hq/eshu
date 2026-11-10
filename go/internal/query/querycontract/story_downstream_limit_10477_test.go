// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// TestBoundedDownstreamReadLimit is the #10477 Q2 RED test: the explicit
// downstream bound defaults to the shared 25-row search depth, stays in
// (0,100], and clamps an over-max caller to the ceiling while echoing it.
func TestBoundedDownstreamReadLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"unset defaults to the shared search depth", 0, DefaultIndirectEvidenceSearchLimit},
		{"negative defaults to the shared search depth", -3, DefaultIndirectEvidenceSearchLimit},
		{"one is the smallest explicit bound", 1, 1},
		{"thirty recovers a thirty-consumer fan-out", 30, 30},
		{"ceiling is honored exactly", MaxIndirectEvidenceSearchLimit, MaxIndirectEvidenceSearchLimit},
		{"over-max clamps to the ceiling", MaxIndirectEvidenceSearchLimit + 1, MaxIndirectEvidenceSearchLimit},
		{"large over-max clamps to the ceiling", 500, MaxIndirectEvidenceSearchLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BoundedDownstreamReadLimit(tc.limit); got != tc.want {
				t.Fatalf("BoundedDownstreamReadLimit(%d) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}
