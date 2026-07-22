// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBindFluxControllersToCrossRepoTargetsFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		controller map[string]any
		sources    []map[string]any
		wantTally  fluxTargetAttributionTally
	}{
		{
			name:       "missing matching Flux GitRepository evidence",
			controller: fluxTargetTestController("GitRepository", "app-source"),
			sources:    []map[string]any{fluxTargetTestSource("repo-app", []string{"other-source"})},
			wantTally:  fluxTargetAttributionTally{Missing: 1},
		},
		{
			name:       "missing Flux sourceRef name",
			controller: fluxTargetTestController("GitRepository", ""),
			sources:    []map[string]any{fluxTargetTestSource("repo-app", []string{"app-source"})},
			wantTally:  fluxTargetAttributionTally{Missing: 1},
		},
		{
			name:       "ambiguous matching targets",
			controller: fluxTargetTestController("GitRepository", "app-source"),
			sources: []map[string]any{
				fluxTargetTestSource("repo-app-a", []string{"app-source"}),
				fluxTargetTestSource("repo-app-b", []string{"app-source"}),
			},
			wantTally: fluxTargetAttributionTally{Ambiguous: 1},
		},
		{
			name:       "binding query saturation",
			controller: fluxTargetTestController("GitRepository", "app-source"),
			sources: []map[string]any{{
				"relationship_type":              "DEPLOYS_FROM",
				"source_id":                      "repo-deploy",
				"target_id":                      "repo-app",
				"flux_target_bindings_saturated": true,
			}},
			wantTally: fluxTargetAttributionTally{Saturated: 1},
		},
		{
			name:       "non GitRepository Flux source",
			controller: fluxTargetTestController("HelmRepository", "app-source"),
			sources:    []map[string]any{fluxTargetTestSource("repo-app", []string{"app-source"})},
			wantTally:  fluxTargetAttributionTally{Unsupported: 1},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tally := bindFluxControllersToCrossRepoTargets([]map[string]any{tt.controller}, tt.sources)
			if tally != tt.wantTally {
				t.Fatalf("tally = %#v, want %#v", tally, tt.wantTally)
			}
			if got := StringVal(tt.controller, "flux_target_repo_id"); got != "" {
				t.Fatalf("flux_target_repo_id = %q, want omitted", got)
			}
		})
	}
}

func TestCollectDeploymentSourceK8sResourcesRejectsCrossRepoFluxPathsOutsideRoot(t *testing.T) {
	t.Parallel()

	resources, _ := collectDeploymentSourceK8sResources([]map[string]any{{
		"entity_id":           "flux-kustomization:payments",
		"controller_kind":     "flux_kustomization",
		"repo_id":             "repo-deploy",
		"flux_target_repo_id": "repo-app",
		"source_roots":        []string{"apps/payments"},
	}}, []EntityContent{{
		EntityID:     "k8s:other",
		RepoID:       "repo-app",
		EntityType:   "K8sResource",
		RelativePath: "apps/other/deployment.yaml",
		Metadata:     map[string]any{"kind": "Deployment"},
	}})
	if len(resources) != 0 {
		t.Fatalf("resources = %#v, want cross-repo resource outside Flux root rejected", resources)
	}
}

func fluxTargetTestController(sourceRefKind string, sourceRefName string) map[string]any {
	return map[string]any{
		"controller_kind": "flux_kustomization",
		"repo_id":         "repo-deploy",
		"source_ref_kind": sourceRefKind,
		"source_ref_name": sourceRefName,
	}
}

func fluxTargetTestSource(targetRepoID string, names []string) map[string]any {
	return map[string]any{
		"relationship_type":         "DEPLOYS_FROM",
		"source_id":                 "repo-deploy",
		"target_id":                 targetRepoID,
		"flux_git_repository_names": names,
	}
}
