// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestClassifyRelationshipStoryEvidence covers the missing-edge classifier,
// including the two cases a review flagged: a repo-scoped override story (real
// rows under a non-"resolved" status) must not read as target_unresolved, and a
// floor that empties a paged fetch must not claim an exhaustive
// all_below_confidence_floor verdict over a partial page.
func TestClassifyRelationshipStoryEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		in             relationshipStoryEvidenceInputs
		wantReason     string
		wantTruncation string
	}{
		{
			name:           "resolved_complete",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 3, AfterFloorCount: 3},
			wantReason:     relationshipStoryReasonComplete,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "repo_scoped_with_rows_is_not_unresolved",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "repo_scoped", RawCount: 4, AfterFloorCount: 4},
			wantReason:     relationshipStoryReasonComplete,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "unresolved_status_empty_is_target_unresolved",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "ambiguous", RawCount: 0},
			wantReason:     relationshipStoryReasonTargetUnresolved,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "resolved_no_edges",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 0},
			wantReason:     relationshipStoryReasonNoEdges,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_complete_page_is_exhaustive",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 0, FloorApplied: true, RawPaged: false},
			wantReason:     relationshipStoryReasonFloorFiltered,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_paged_page_is_truncated_not_exhaustive",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 0, FloorApplied: true, RawPaged: true},
			wantReason:     relationshipStoryReasonTruncatedLimit,
			wantTruncation: relationshipStoryTruncationCount,
		},
		{
			name:           "paged_fetch_with_rows_is_count_truncated",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 2, RawPaged: true},
			wantReason:     relationshipStoryReasonTruncatedLimit,
			wantTruncation: relationshipStoryTruncationCount,
		},
		{
			name:           "token_budget_truncation",
			in:             relationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 2, BudgetTruncated: true},
			wantReason:     relationshipStoryReasonTruncatedBudget,
			wantTruncation: relationshipStoryTruncationBudget,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRelationshipStoryEvidence(tc.in)
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Truncation != tc.wantTruncation {
				t.Errorf("truncation = %q, want %q", got.Truncation, tc.wantTruncation)
			}
		})
	}
}
