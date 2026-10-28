// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"
)

// TestClassifyRelationshipStoryEvidenceReasons proves the fixed reason
// priority: unresolved targets and empty graphs report before truncation,
// and a floor-emptied page reports the floor verdict only for a complete
// page.
func TestClassifyRelationshipStoryEvidenceReasons(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		inputs RelationshipStoryEvidenceInputs
		reason string
	}{
		{
			name:   "unresolved target",
			inputs: RelationshipStoryEvidenceInputs{ResolutionStatus: "not_found"},
			reason: RelationshipStoryReasonTargetUnresolved,
		},
		{
			name:   "resolved target without edges",
			inputs: RelationshipStoryEvidenceInputs{ResolutionStatus: "resolved"},
			reason: RelationshipStoryReasonNoEdges,
		},
		{
			name: "floor emptied complete page",
			inputs: RelationshipStoryEvidenceInputs{
				ResolutionStatus: "resolved", RawCount: 3, FloorApplied: true,
			},
			reason: RelationshipStoryReasonFloorFiltered,
		},
		{
			name: "floor emptied paged page stays truncated",
			inputs: RelationshipStoryEvidenceInputs{
				ResolutionStatus: "resolved", RawCount: 3,
				FloorApplied: true, RawPaged: true,
			},
			reason: RelationshipStoryReasonTruncatedLimit,
		},
		{
			name: "budget truncation",
			inputs: RelationshipStoryEvidenceInputs{
				ResolutionStatus: "resolved", RawCount: 2, BudgetTruncated: true,
			},
			reason: RelationshipStoryReasonTruncatedBudget,
		},
		{
			name: "complete result",
			inputs: RelationshipStoryEvidenceInputs{
				ResolutionStatus: "repo_scoped", RawCount: 2, AfterFloorCount: 2,
			},
			reason: RelationshipStoryReasonComplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyRelationshipStoryEvidence(tc.inputs); got.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q (truncation %q)", got.Reason, tc.reason, got.Truncation)
			}
		})
	}
}

// TestRelationshipStoryRowsAboveConfidenceFloor proves the optional
// floor keeps only rows at or above the threshold and passes rows
// through untouched when no floor is set.
func TestRelationshipStoryRowsAboveConfidenceFloor(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"confidence": 0.95},
		{"confidence": 0.50},
		{"name": "legacy without confidence"},
		{"confidence": "high"},
	}
	floor := 0.7
	filtered := RelationshipStoryRowsAboveConfidenceFloor(rows, RelationshipStoryRequest{MinConfidence: &floor})
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}

	passthrough := RelationshipStoryRowsAboveConfidenceFloor(rows, RelationshipStoryRequest{})
	if len(passthrough) != len(rows) {
		t.Errorf("nil floor must pass %d rows through, got %d", len(rows), len(passthrough))
	}
}

// TestRelationshipStoryRequestValidate proves the story bounds reject
// missing targets, bad directions, bad types, and illegal transitive
// combinations.
func TestRelationshipStoryRequestValidate(t *testing.T) {
	t.Parallel()

	if err := (RelationshipStoryRequest{}).Validate(); err == nil {
		t.Error("targetless request must fail validation")
	}
	if err := (RelationshipStoryRequest{Target: "f", Direction: "sideways"}).Validate(); err == nil {
		t.Error("bad direction must fail validation")
	}
	if err := (RelationshipStoryRequest{Target: "f", RelationshipType: "CALLS_ON"}).Validate(); err == nil {
		t.Error("unsupported relationship type must fail validation")
	}
	if err := (RelationshipStoryRequest{Target: "f", IncludeTransitive: true, Offset: 5}).Validate(); err == nil {
		t.Error("transitive reads require offset 0")
	}
	if err := (RelationshipStoryRequest{Target: "f", IncludeTransitive: true, Direction: "outgoing"}).Validate(); err != nil {
		t.Errorf("directed CALLS transitive request must validate: %v", err)
	}
	if err := (RelationshipStoryRequest{Target: "f", IncludeTransitive: true}).Validate(); err == nil {
		t.Error("undirected transitive request must fail validation")
	}
	if err := (RelationshipStoryRequest{QueryType: "overrides"}).Validate(); err != nil {
		t.Errorf("repo-scoped overrides story must validate without a target: %v", err)
	}
}

// TestRelationshipStoryRequestNormalizers proves the target, limit,
// direction, type, and budget normalizers and their defaults.
func TestRelationshipStoryRequestNormalizers(t *testing.T) {
	t.Parallel()

	req := RelationshipStoryRequest{Name: " main ", Limit: 9999}
	if got := req.EffectiveTarget(); got != "main" {
		t.Errorf("EffectiveTarget = %q, want main", got)
	}
	if got := req.NormalizedLimit(); got != relationshipStoryMaxLimit {
		t.Errorf("NormalizedLimit = %d, want the max %d", got, relationshipStoryMaxLimit)
	}
	if got, err := req.NormalizedDirection(); err != nil || got != "both" {
		t.Errorf("default NormalizedDirection = %q, %v; want both, nil", got, err)
	}
	if got, err := req.NormalizedRelationshipType(); err != nil || got != "CALLS" {
		t.Errorf("default NormalizedRelationshipType = %q, %v; want CALLS, nil", got, err)
	}
	types, err := RelationshipStoryRequest{RelationshipTypes: []string{"calls", "CALLS", " imports "}}.NormalizedRelationshipTypes()
	if err != nil {
		t.Fatalf("NormalizedRelationshipTypes failed: %v", err)
	}
	if len(types) != 2 || types[0] != "CALLS" || types[1] != "IMPORTS" {
		t.Errorf("NormalizedRelationshipTypes = %v, want [CALLS IMPORTS]", types)
	}
	if got := (RelationshipStoryRequest{TokenBudget: -1}).NormalizedTokenBudget(); got != 0 {
		t.Errorf("negative NormalizedTokenBudget = %d, want 0", got)
	}
}
