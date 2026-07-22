// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
)

func TestFetchFluxDeploymentSourceTargetBindingsIsBoundedAndEvidenceSpecific(t *testing.T) {
	t.Parallel()

	var seenCyphers []string
	var seenParams []map[string]any
	call := 0
	result, err := fetchFluxDeploymentSourceTargetBindings(context.Background(), fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
	}, "repo-app", []string{"repo-deploy"}, contextStoryItemLimit+1, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("fetchFluxDeploymentSourceTargetBindings() error = %v", err)
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
	if got, want := seenParams[0]["source_limit"], contextStoryItemLimit+1; got != want {
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
	access := repositoryAccessFilter{allowedRepositoryIDs: []string{"repo-deploy", "repo-app"}, allowed: map[string]struct{}{"repo-deploy": {}, "repo-app": {}}}
	_, err := fetchFluxDeploymentSourceTargetBindings(t.Context(), fakeRepoGraphReader{run: func(_ context.Context, got string, _ map[string]any) ([]map[string]any, error) {
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

	sources := attachFluxDeploymentSourceTargetBindings([]map[string]any{{
		"relationship_type": "DEPLOYS_FROM", "source_id": "repo-deploy", "target_id": "repo-app",
	}}, nil, true)
	if len(sources) != 1 || !BoolVal(sources[0], "flux_target_bindings_saturated") {
		t.Fatalf("sources = %#v, want saturation marker", sources)
	}
}

func TestFetchDeploymentSourceResultReportsFirstHopSaturationWhenTargetExpansionReturnsFewerRows(t *testing.T) {
	t.Parallel()

	expansionCalled := false
	result, err := fetchDeploymentSourceResultFromGraph(t.Context(), fakeRepoGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "DEPLOYMENT_SOURCE"):
			return nil, nil
		case strings.Contains(cypher, "[rel:DEPLOYS_FROM]"):
			return []map[string]any{{"repo_id": "repo-deploy", "repo_name": "deploy", "confidence": 0.99}}, nil
		case strings.Contains(cypher, "artifact.id AS artifact_id"):
			// The first hop reached the 51-row sentinel, but only 50 artifacts
			// expanded to the requested target. Saturation must not disappear.
			rows := make([]map[string]any, contextStoryItemLimit+1)
			for i := range rows {
				rows[i] = map[string]any{
					"source_id": "repo-deploy", "artifact_id": fmt.Sprintf("artifact-%02d", i),
					"flux_git_repository_namespace": "flux-system",
					"flux_git_repository_name":      fmt.Sprintf("source-%02d", i),
				}
			}
			return rows, nil
		case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"):
			expansionCalled = true
			return make([]map[string]any, contextStoryItemLimit), nil
		default:
			return nil, nil
		}
	}}, "workload-app", "repo-app")
	if err != nil {
		t.Fatal(err)
	}
	if expansionCalled {
		t.Fatal("target expansion ran after first-hop saturation; sentinel artifact could enter attribution")
	}
	if !BoolVal(result.limits, "flux_target_binding_observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want first-hop saturation lower bound", result.limits)
	}
	if len(result.rows) != 1 || !BoolVal(result.rows[0], "flux_target_bindings_saturated") {
		t.Fatalf("rows = %#v, want saturated source with no partial attribution", result.rows)
	}
}
