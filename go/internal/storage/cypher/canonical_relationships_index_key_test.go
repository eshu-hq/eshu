// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestEvidenceArtifactEnvironmentRowDropsWholeRow pins what an oversized value
// costs on the production statement (#7058 review N2): the guard removes the
// whole row, so the EvidenceArtifact node and both relationships that row
// writes are dropped along with the Environment node whose name was too large.
func TestEvidenceArtifactEnvironmentRowDropsWholeRow(t *testing.T) {
	rows := []map[string]any{
		{"repo_id": "r1", "target_repo_id": "r2", "artifact_id": "artifact-ok", "environment": "prod"},
		{"repo_id": "r1", "target_repo_id": "r2", "artifact_id": "artifact-big", "environment": strings.Repeat("e", 9000)},
	}
	out, dropped, skip := graph.GuardIndexKeyWrites(BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher, map[string]any{"rows": rows})
	if skip {
		t.Fatal("skip = true, want row-level drop")
	}
	if len(dropped) != 1 || dropped[0].Label != "Environment" || dropped[0].Property != "name" {
		t.Fatalf("dropped = %+v, want one Environment/name", dropped)
	}
	kept := out["rows"].([]map[string]any)
	if len(kept) != 1 || kept[0]["artifact_id"] != "artifact-ok" {
		t.Fatalf("kept rows = %+v, want only artifact-ok: the artifact-big row (artifact node and both edges) must go with its Environment", kept)
	}
	if refs, _ := graph.UnanalyzedIndexWrites(BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher); len(refs) != 0 {
		t.Fatalf("production statement reported unanalyzed: %+v", refs)
	}
}

// The rationale query is assembled from a label disjunction. Pin the guard's
// behavior on the complete production statement, not a Go string fragment.
func TestCanonicalRationaleUIDRowDropsWholeRow(t *testing.T) {
	rows := []map[string]any{
		{"rationale_uid": "rationale:ok", "target_entity_id": "entity:ok"},
		{"rationale_uid": strings.Repeat("r", graph.MaxIndexKeyBytes+1), "target_entity_id": "entity:big"},
	}
	out, dropped, skip := graph.GuardIndexKeyWrites(BatchCanonicalRationaleExplainsEdgeCypher, map[string]any{"rows": rows})
	if skip {
		t.Fatal("skip = true, want row-level drop")
	}
	if len(dropped) != 1 || dropped[0].Label != "Rationale" || dropped[0].Property != "uid" {
		t.Fatalf("dropped = %+v, want one Rationale/uid", dropped)
	}
	kept := out["rows"].([]map[string]any)
	if len(kept) != 1 || kept[0]["rationale_uid"] != "rationale:ok" {
		t.Fatalf("kept rows = %+v, want only rationale:ok", kept)
	}
	if refs, _ := graph.UnanalyzedIndexWrites(BatchCanonicalRationaleExplainsEdgeCypher); len(refs) != 0 {
		t.Fatalf("production statement reported unanalyzed: %+v", refs)
	}
}
