// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestRepoEvidenceArtifactRowsPreserveQualifiedFluxIdentity(t *testing.T) {
	t.Parallel()

	rows := repoEvidenceArtifactRowsFromIntent(reducer.SharedProjectionIntentRow{
		GenerationID: "gen-5540",
		Payload: map[string]any{
			"repo_id":           "repo-deploy",
			"target_repo_id":    "repo-app",
			"relationship_type": "DEPLOYS_FROM",
			"resolved_id":       "resolved-5540",
			"evidence_artifacts": []map[string]any{{
				"evidence_kind":                 "FLUX_GIT_REPOSITORY_SOURCE",
				"matched_alias":                 "app-source",
				"matched_value":                 "https://example.test/app.git",
				"flux_git_repository_name":      "app-source",
				"flux_git_repository_namespace": "flux-system",
			}},
		},
	}, "resolver/cross-repo")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0]["matched_alias"], "app-source"; got != want {
		t.Fatalf("matched_alias = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["flux_git_repository_namespace"], "flux-system"; got != want {
		t.Fatalf("flux_git_repository_namespace = %#v, want %#v", got, want)
	}
	other := repoEvidenceArtifactID("resolved-5540", "FLUX_GIT_REPOSITORY_SOURCE", "", "https://example.test/app.git", "other", "app-source")
	if rows[0]["artifact_id"] == other {
		t.Fatal("qualified artifact identities collapsed across namespaces")
	}
}

func TestFluxArtifactUpsertProjectsDedicatedIdentityProperties(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"artifact.flux_git_repository_name = row.flux_git_repository_name", "artifact.flux_git_repository_namespace = row.flux_git_repository_namespace"} {
		if !strings.Contains(batchCanonicalRepoEvidenceArtifactUpsertCypher, want) {
			t.Fatalf("upsert missing %q", want)
		}
	}
}
