// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
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
				"source_id": "repo-deploy", "target_id": "repo-app", "flux_git_repository_namespace": "flux-system", "flux_git_repository_name": "app-source",
			}}, nil
		},
	}, "repo-app", []string{"repo-deploy"}, contextStoryItemLimit+1, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("fetchFluxDeploymentSourceTargetBindings() error = %v", err)
	}
	for _, want := range []string{
		"UNWIND $source_repo_ids AS source_id",
		"MATCH (repo:Repository {id: source_id})",
		"artifact.evidence_kind = 'FLUX_GIT_REPOSITORY_SOURCE'",
		"artifact.flux_git_repository_namespace",
		"artifact.flux_git_repository_name",
		"LIMIT $source_limit",
	} {
		if !strings.Contains(seenCypher, want) {
			t.Fatalf("binding query missing %q: %s", want, seenCypher)
		}
	}
	if strings.Index(seenCypher, "LIMIT $source_limit") > strings.Index(seenCypher, "EVIDENCES_REPOSITORY_RELATIONSHIP") {
		t.Fatalf("sentinel must precede target expansion: %s", seenCypher)
	}
	if strings.Contains(seenCypher, "RETURN DISTINCT") || strings.Contains(seenCypher, "artifact.matched_alias") {
		t.Fatalf("binding query used collapsing/generic identity shape: %s", seenCypher)
	}
	if got := seenParams["source_repo_ids"]; !reflect.DeepEqual(got, []string{"repo-deploy"}) {
		t.Fatalf("source_repo_ids = %#v", got)
	}
	if got, want := seenParams["source_limit"], contextStoryItemLimit+1; got != want {
		t.Fatalf("source_limit = %#v, want %#v", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
}

func TestFetchFluxDeploymentSourceTargetBindingsScopedQueryHasOneWherePerMatch(t *testing.T) {
	t.Parallel()
	var cypher string
	access := repositoryAccessFilter{allowedRepositoryIDs: []string{"repo-deploy", "repo-app"}, allowed: map[string]struct{}{"repo-deploy": {}, "repo-app": {}}}
	_, err := fetchFluxDeploymentSourceTargetBindings(t.Context(), fakeRepoGraphReader{run: func(_ context.Context, got string, _ map[string]any) ([]map[string]any, error) {
		cypher = got
		return nil, nil
	}}, "repo-app", []string{"repo-deploy"}, 51, access)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(cypher, "WHERE "); got != 2 {
		t.Fatalf("WHERE count = %d, want one per MATCH stage: %s", got, cypher)
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
