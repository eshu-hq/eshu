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
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 3, afterFloorCount: 3},
			wantReason:     relationshipStoryReasonComplete,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "repo_scoped_with_rows_is_not_unresolved",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "repo_scoped", rawCount: 4, afterFloorCount: 4},
			wantReason:     relationshipStoryReasonComplete,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "unresolved_status_empty_is_target_unresolved",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "ambiguous", rawCount: 0},
			wantReason:     relationshipStoryReasonTargetUnresolved,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "resolved_no_edges",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 0},
			wantReason:     relationshipStoryReasonNoEdges,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_complete_page_is_exhaustive",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 2, afterFloorCount: 0, floorApplied: true, rawPaged: false},
			wantReason:     relationshipStoryReasonFloorFiltered,
			wantTruncation: relationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_paged_page_is_truncated_not_exhaustive",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 2, afterFloorCount: 0, floorApplied: true, rawPaged: true},
			wantReason:     relationshipStoryReasonTruncatedLimit,
			wantTruncation: relationshipStoryTruncationCount,
		},
		{
			name:           "paged_fetch_with_rows_is_count_truncated",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 2, afterFloorCount: 2, rawPaged: true},
			wantReason:     relationshipStoryReasonTruncatedLimit,
			wantTruncation: relationshipStoryTruncationCount,
		},
		{
			name:           "token_budget_truncation",
			in:             relationshipStoryEvidenceInputs{resolutionStatus: "resolved", rawCount: 2, afterFloorCount: 2, budgetTruncated: true},
			wantReason:     relationshipStoryReasonTruncatedBudget,
			wantTruncation: relationshipStoryTruncationBudget,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRelationshipStoryEvidence(tc.in)
			if got.reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.reason, tc.wantReason)
			}
			if got.truncation != tc.wantTruncation {
				t.Errorf("truncation = %q, want %q", got.truncation, tc.wantTruncation)
			}
		})
	}
}
