// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderRelationshipEvidenceByResolvedIDHydratesDetails(t *testing.T) {
	t.Parallel()

	details := []byte(`{
		"evidence_kinds": ["GITHUB_ACTIONS_REUSABLE_WORKFLOW", "HELM_VALUES_REFERENCE"],
		"evidence_preview": [
			{
				"kind": "GITHUB_ACTIONS_REUSABLE_WORKFLOW",
				"confidence": 0.88,
				"details": {
					"path": ".github/workflows/deploy.yaml",
					"matched_value": "shared-deploy.yaml",
					"start_line": 7
				}
			}
		]
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderRelationshipEvidenceColumns(),
			rows: [][]driver.Value{
				{
					"resolved-1",
					"gen-1",
					"repo-service",
					"checkout-service",
					"entity-service",
					"repo-platform",
					"platform-infra",
					"entity-platform",
					"DEPENDS_ON",
					0.88,
					int64(2),
					"matched deployment and runtime evidence",
					"relationship_resolver",
					details,
					"repository",
					"run-1",
					"active",
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.RelationshipEvidenceByResolvedID(t.Context(), "resolved-1")
	if err != nil {
		t.Fatalf("RelationshipEvidenceByResolvedID() error = %v, want nil", err)
	}
	if !got.Available {
		t.Fatal("RelationshipEvidenceByResolvedID().Available = false, want true")
	}
	row := got.Row
	if got, want := row["resolved_id"], "resolved-1"; got != want {
		t.Fatalf("resolved_id = %#v, want %#v", got, want)
	}
	if got, want := row["lookup_basis"], "resolved_id"; got != want {
		t.Fatalf("lookup_basis = %#v, want %#v", got, want)
	}
	if got, want := row["relationship_type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := row["confidence_basis"], "evidence_aggregate"; got != want {
		t.Fatalf("confidence_basis = %#v, want %#v", got, want)
	}
	source := row["source"].(map[string]any)
	if got, want := source["repo_name"], "checkout-service"; got != want {
		t.Fatalf("source.repo_name = %#v, want %#v", got, want)
	}
	target := row["target"].(map[string]any)
	if got, want := target["entity_id"], "entity-platform"; got != want {
		t.Fatalf("target.entity_id = %#v, want %#v", got, want)
	}
	if kinds := row["evidence_kinds"].([]string); len(kinds) != 2 || kinds[0] != "GITHUB_ACTIONS_REUSABLE_WORKFLOW" {
		t.Fatalf("evidence_kinds = %#v, want decoded details kinds", kinds)
	}
	if previews := row["evidence_preview"].([]any); len(previews) != 1 {
		t.Fatalf("evidence_preview = %#v, want one preview", previews)
	}
}
