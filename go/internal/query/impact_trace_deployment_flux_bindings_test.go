// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

func TestFetchFluxDeploymentSourceTargetBindingsIsBoundedAndEvidenceSpecific(t *testing.T) {
	t.Parallel()

	var seenCypher string
	var seenParams map[string]any
	rows, err := fetchFluxDeploymentSourceTargetBindings(context.Background(), fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			seenCypher = cypher
			seenParams = params
			return []map[string]any{{
				"source_id": "repo-deploy", "target_id": "repo-app", "flux_git_repository_name": "app-source",
			}}, nil
		},
	}, "repo-app", contextStoryItemLimit+1, repositoryAccessFilter{})
	if err != nil {
		t.Fatalf("fetchFluxDeploymentSourceTargetBindings() error = %v", err)
	}
	for _, want := range []string{
		"MATCH (targetRepo:Repository {id: $repo_id})",
		"artifact.evidence_kind = 'FLUX_GIT_REPOSITORY_SOURCE'",
		"artifact.matched_alias",
		"ORDER BY source_id, target_id, flux_git_repository_name",
		"LIMIT $source_limit",
	} {
		if !strings.Contains(seenCypher, want) {
			t.Fatalf("binding query missing %q: %s", want, seenCypher)
		}
	}
	if got, want := seenParams["source_limit"], contextStoryItemLimit+1; got != want {
		t.Fatalf("source_limit = %#v, want %#v", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
}

func TestAttachFluxDeploymentSourceTargetBindingsMarksSaturation(t *testing.T) {
	t.Parallel()

	sources := attachFluxDeploymentSourceTargetBindings([]map[string]any{{
		"relationship_type": "DEPLOYS_FROM", "source_id": "repo-deploy", "target_id": "repo-app",
	}}, nil, true)
	if len(sources) != 1 || !BoolVal(sources[0], "flux_target_bindings_saturated") {
		t.Fatalf("sources = %#v, want saturation marker", sources)
	}
}
