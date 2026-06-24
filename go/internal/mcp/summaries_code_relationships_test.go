// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

// codeRelationshipsData builds an analyze_code_relationships structured payload
// with the given relationship provenance truth states and coverage fields,
// mirroring what the HTTP relationship-story handler returns (issue #3158).
func codeRelationshipsData(truthStates []string, missingReason, explanation string) map[string]any {
	rels := make([]any, 0, len(truthStates))
	for _, state := range truthStates {
		rels = append(rels, map[string]any{
			"provenance": map[string]any{"truth_state": state},
		})
	}
	return map[string]any{
		"relationships": rels,
		"coverage": map[string]any{
			"missing_edge_reason":  missingReason,
			"evidence_explanation": explanation,
		},
	}
}

// TestSummarizeCodeRelationships proves the MCP convenience summary makes
// code-relationship uncertainty explicit across the high-confidence, ambiguous,
// unsupported, and truncated cases — and never relabels heuristic or unsupported
// edges as canonical truth.
func TestSummarizeCodeRelationships(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        map[string]any
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "high_confidence_all_derived",
			data:        codeRelationshipsData([]string{"derived", "derived"}, "complete", "all matching relationships were returned"),
			wantContain: []string{"2 relationship(s)", "2 derived", "0 heuristic", "0 unsupported"},
			// A complete result must not advertise a missing-edge reason.
			wantAbsent: []string{"complete:"},
		},
		{
			name:        "ambiguous_heuristic_present",
			data:        codeRelationshipsData([]string{"derived", "heuristic"}, "complete", ""),
			wantContain: []string{"1 heuristic/ambiguous", "correlation evidence, not canonical code truth"},
		},
		{
			name:        "unsupported_present",
			data:        codeRelationshipsData([]string{"unsupported"}, "complete", ""),
			wantContain: []string{"1 unsupported", "no recorded confidence"},
		},
		{
			name: "truncated_explains_reason",
			data: codeRelationshipsData([]string{"derived"}, "truncated_by_limit",
				"more relationships exist than the limit; raise limit or page with offset"),
			wantContain: []string{"truncated_by_limit", "raise limit or page with offset"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := summarizeCodeRelationships(tc.data)
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("summary %q missing %q", got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("summary %q should not contain %q", got, absent)
				}
			}
		})
	}
}
