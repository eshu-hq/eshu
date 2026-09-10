// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// TestClassifyRelationshipStoryEvidence covers the missing-edge classifier,
// including the two cases a review flagged: a repo-scoped override story (real
// rows under a non-"resolved" status) must not read as target_unresolved, and a
// floor that empties a paged fetch must not claim an exhaustive
// all_below_confidence_floor verdict over a partial page.
func TestClassifyRelationshipStoryEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		in             codemodel.RelationshipStoryEvidenceInputs
		wantReason     string
		wantTruncation string
	}{
		{
			name:           "resolved_complete",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 3, AfterFloorCount: 3},
			wantReason:     codemodel.RelationshipStoryReasonComplete,
			wantTruncation: codemodel.RelationshipStoryTruncationNone,
		},
		{
			name:           "repo_scoped_with_rows_is_not_unresolved",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "repo_scoped", RawCount: 4, AfterFloorCount: 4},
			wantReason:     codemodel.RelationshipStoryReasonComplete,
			wantTruncation: codemodel.RelationshipStoryTruncationNone,
		},
		{
			name:           "unresolved_status_empty_is_target_unresolved",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "ambiguous", RawCount: 0},
			wantReason:     codemodel.RelationshipStoryReasonTargetUnresolved,
			wantTruncation: codemodel.RelationshipStoryTruncationNone,
		},
		{
			name:           "resolved_no_edges",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 0},
			wantReason:     codemodel.RelationshipStoryReasonNoEdges,
			wantTruncation: codemodel.RelationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_complete_page_is_exhaustive",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 0, FloorApplied: true, RawPaged: false},
			wantReason:     codemodel.RelationshipStoryReasonFloorFiltered,
			wantTruncation: codemodel.RelationshipStoryTruncationNone,
		},
		{
			name:           "floor_empties_paged_page_is_truncated_not_exhaustive",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 0, FloorApplied: true, RawPaged: true},
			wantReason:     codemodel.RelationshipStoryReasonTruncatedLimit,
			wantTruncation: codemodel.RelationshipStoryTruncationCount,
		},
		{
			name:           "paged_fetch_with_rows_is_count_truncated",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 2, RawPaged: true},
			wantReason:     codemodel.RelationshipStoryReasonTruncatedLimit,
			wantTruncation: codemodel.RelationshipStoryTruncationCount,
		},
		{
			name:           "token_budget_truncation",
			in:             codemodel.RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved", RawCount: 2, AfterFloorCount: 2, BudgetTruncated: true},
			wantReason:     codemodel.RelationshipStoryReasonTruncatedBudget,
			wantTruncation: codemodel.RelationshipStoryTruncationBudget,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codemodel.ClassifyRelationshipStoryEvidence(tc.in)
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Truncation != tc.wantTruncation {
				t.Errorf("truncation = %q, want %q", got.Truncation, tc.wantTruncation)
			}
		})
	}
}
