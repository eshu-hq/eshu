// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestRepoEvidenceArtifactRowsPreserveFluxGitRepositoryName(t *testing.T) {
	t.Parallel()

	rows := repoEvidenceArtifactRowsFromIntent(reducer.SharedProjectionIntentRow{
		GenerationID: "gen-5540",
		Payload: map[string]any{
			"repo_id":           "repo-deploy",
			"target_repo_id":    "repo-app",
			"relationship_type": "DEPLOYS_FROM",
			"resolved_id":       "resolved-5540",
			"evidence_artifacts": []map[string]any{{
				"evidence_kind": "FLUX_GIT_REPOSITORY_SOURCE",
				"matched_alias": "app-source",
				"matched_value": "https://example.test/app.git",
			}},
		},
	}, "resolver/cross-repo")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0]["matched_alias"], "app-source"; got != want {
		t.Fatalf("matched_alias = %#v, want %#v", got, want)
	}
}
