// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRepoEvidenceArtifactRowsFromIntentCarriesRefFields proves that
// repoEvidenceArtifactRowsFromIntent copies the reducer-computed
// ref_value/ref_pinned fields (issue #5372) from an evidence-artifact map
// onto the graph-write row, following the same copy-through pattern already
// used for start_line/end_line/commit_sha. The reducer
// (resolvedRelationshipEvidenceArtifacts) is the sole place these fields are
// computed; this builder only carries them, it never recomputes Pinned().
func TestRepoEvidenceArtifactRowsFromIntentCarriesRefFields(t *testing.T) {
	t.Parallel()

	row := reducer.SharedProjectionIntentRow{
		GenerationID: "gen-1",
		Payload: map[string]any{
			"repo_id":           "repo-service",
			"target_repo_id":    "repo-action",
			"relationship_type": "DEPENDS_ON",
			"resolved_id":       "resolved-1",
			"evidence_artifacts": []map[string]any{
				{
					"evidence_kind": "GITHUB_ACTIONS_ACTION_REPOSITORY",
					"path":          ".github/workflows/deploy.yml",
					"matched_value": "octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
					"ref_value":     "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
					"ref_pinned":    true,
				},
			},
		},
	}

	rows := repoEvidenceArtifactRowsFromIntent(row, "resolver/cross-repo")
	if len(rows) != 1 {
		t.Fatalf("repoEvidenceArtifactRowsFromIntent() len = %d, want 1", len(rows))
	}
	got := rows[0]

	if v, _ := got["ref_value"].(string); v != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("row[ref_value] = %v, want the full SHA", got["ref_value"])
	}
	if v, ok := got["ref_pinned"].(bool); !ok || !v {
		t.Errorf("row[ref_pinned] = %v (%T), want true", got["ref_pinned"], got["ref_pinned"])
	}
}

// TestRepoEvidenceArtifactRowsFromIntentNilsRefFieldsWhenAbsent proves that
// when the reducer-computed artifact map carries no ref_value (a local
// workflow, docker action, or non-GitHub-Actions evidence kind), the graph
// write row sends both fields as an explicit nil rather than fabricating
// ref_pinned:false or ref_value:"". The keys must be present: the pinned
// NornicDB v1.3.3 stores the literal text "row.ref_value" for a missing UNWIND
// row key (#6782).
func TestRepoEvidenceArtifactRowsFromIntentNilsRefFieldsWhenAbsent(t *testing.T) {
	t.Parallel()

	row := reducer.SharedProjectionIntentRow{
		GenerationID: "gen-1",
		Payload: map[string]any{
			"repo_id":           "repo-service",
			"target_repo_id":    "repo-service",
			"relationship_type": "DEPLOYS_FROM",
			"resolved_id":       "resolved-2",
			"evidence_artifacts": []map[string]any{
				{
					"evidence_kind": "GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW",
					"path":          ".github/workflows/deploy.yml",
					"matched_value": ".github/workflows/deploy.yml",
					// No ref_value/ref_pinned -- local workflow has no @ref.
				},
			},
		},
	}

	rows := repoEvidenceArtifactRowsFromIntent(row, "resolver/cross-repo")
	if len(rows) != 1 {
		t.Fatalf("repoEvidenceArtifactRowsFromIntent() len = %d, want 1", len(rows))
	}
	got := rows[0]

	for _, key := range []string{"ref_value", "ref_pinned"} {
		if v, ok := got[key]; !ok || v != nil {
			t.Errorf("row[%s] = %#v (present=%v), want an explicit nil (no fabrication)", key, v, ok)
		}
	}
}
