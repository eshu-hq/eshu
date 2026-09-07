// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestFetchFluxDeploymentSourceTargetBindingsIsBoundedAndEvidenceSpecific(t *testing.T) {
	t.Parallel()

	var seenCyphers []string
	var seenParams []map[string]any
	call := 0
	result, err := FetchFluxDeploymentSourceTargetBindings(context.Background(), querytestutil.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			seenCyphers = append(seenCyphers, cypher)
			seenParams = append(seenParams, maps.Clone(params))
			call++
			if call == 1 {
				return []map[string]any{{
					"source_id": "repo-deploy", "artifact_id": "artifact-1",
					"flux_git_repository_namespace": "flux-system", "flux_git_repository_name": "app-source",
				}}, nil
			}
			return []map[string]any{{
				"source_id": "repo-deploy", "target_id": "repo-app", "flux_git_repository_namespace": "flux-system", "flux_git_repository_name": "app-source",
			}}, nil
		},
	}, "repo-app", []string{"repo-deploy"}, querycontract.ContextStoryItemLimit+1, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("FetchFluxDeploymentSourceTargetBindings() error = %v", err)
	}
	seenCypher := strings.Join(seenCyphers, "\n")
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
	if strings.Contains(seenCyphers[0], "EVIDENCES_REPOSITORY_RELATIONSHIP") || !strings.Contains(seenCyphers[1], "EVIDENCES_REPOSITORY_RELATIONSHIP") {
		t.Fatalf("first-hop sentinel and target expansion were not split: %#v", seenCyphers)
	}
	if strings.Contains(seenCypher, "RETURN DISTINCT") || strings.Contains(seenCypher, "artifact.matched_alias") {
		t.Fatalf("binding query used collapsing/generic identity shape: %s", seenCypher)
	}
	if got := seenParams[0]["source_repo_ids"]; !reflect.DeepEqual(got, []string{"repo-deploy"}) {
		t.Fatalf("source_repo_ids = %#v", got)
	}
	if got, want := seenParams[0]["source_limit"], querycontract.ContextStoryItemLimit+1; got != want {
		t.Fatalf("source_limit = %#v, want %#v", got, want)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if result.firstHopSaturated || result.firstHopCount != 1 {
		t.Fatalf("result = %#v, want one complete first-hop row", result)
	}
}

func TestFetchFluxDeploymentSourceTargetBindingsScopedQueryHasOneWherePerMatch(t *testing.T) {
	t.Parallel()
	var cyphers []string
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-deploy", "repo-app"}, Allowed: map[string]struct{}{"repo-deploy": {}, "repo-app": {}}}
	_, err := FetchFluxDeploymentSourceTargetBindings(t.Context(), querytestutil.FakeRepoGraphReader{RunFn: func(_ context.Context, got string, _ map[string]any) ([]map[string]any, error) {
		cyphers = append(cyphers, got)
		if len(cyphers) == 1 {
			return []map[string]any{{"artifact_id": "artifact-1"}}, nil
		}
		return nil, nil
	}}, "repo-app", []string{"repo-deploy"}, 51, access)
	if err != nil {
		t.Fatal(err)
	}
	for _, cypher := range cyphers {
		if got := strings.Count(cypher, "WHERE "); got != 1 {
			t.Fatalf("WHERE count = %d, want one per query: %s", got, cypher)
		}
	}
}

func TestAttachFluxDeploymentSourceTargetBindingsMarksSaturation(t *testing.T) {
	t.Parallel()

	sources := AttachFluxDeploymentSourceTargetBindings([]map[string]any{{
		"relationship_type": "DEPLOYS_FROM", "source_id": "repo-deploy", "target_id": "repo-app",
	}}, nil, true)
	if len(sources) != 1 || !querycontract.BoolVal(sources[0], "flux_target_bindings_saturated") {
		t.Fatalf("sources = %#v, want saturation marker", sources)
	}
}
