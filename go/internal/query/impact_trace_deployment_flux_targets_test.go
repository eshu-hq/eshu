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
			sources:    []map[string]any{fluxTargetTestSource("repo-app", "flux-system", "other-source")},
			wantTally:  fluxTargetAttributionTally{Missing: 1},
		},
		{
			name:       "missing Flux sourceRef name",
			controller: fluxTargetTestController("GitRepository", ""),
			sources:    []map[string]any{fluxTargetTestSource("repo-app", "flux-system", "app-source")},
			wantTally:  fluxTargetAttributionTally{Missing: 1},
		},
		{
			name:       "missing effective namespace",
			controller: map[string]any{"controller_kind": "flux_kustomization", "repo_id": "repo-deploy", "source_ref_kind": "GitRepository", "source_ref_name": "app-source"},
			sources:    []map[string]any{fluxTargetTestSource("repo-app", "flux-system", "app-source")},
			wantTally:  fluxTargetAttributionTally{Missing: 1},
		},
		{
			name:       "ambiguous matching targets",
			controller: fluxTargetTestController("GitRepository", "app-source"),
			sources: []map[string]any{
				fluxTargetTestSource("repo-app-a", "flux-system", "app-source"),
				fluxTargetTestSource("repo-app-b", "flux-system", "app-source"),
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
			sources:    []map[string]any{fluxTargetTestSource("repo-app", "flux-system", "app-source")},
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

func TestBindFluxControllersUsesExplicitAndControllerNamespaceExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		controller map[string]any
		want       string
	}{
		{"explicit", map[string]any{"source_ref_namespace": "team-a", "namespace": "wrong"}, "repo-a"},
		{"defaulted", map[string]any{"namespace": "team-b"}, "repo-b"},
	}
	sources := []map[string]any{fluxTargetTestSource("repo-a", "team-a", "app-source"), fluxTargetTestSource("repo-b", "team-b", "app-source")}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			controller := fluxTargetTestController("GitRepository", "app-source")
			for key, value := range tt.controller {
				controller[key] = value
			}
			tally := bindFluxControllersToCrossRepoTargets([]map[string]any{controller}, sources)
			if tally.Linked != 1 || StringVal(controller, "flux_target_repo_id") != tt.want {
				t.Fatalf("controller/tally = %#v/%#v", controller, tally)
			}
		})
	}
}

func TestBindFluxControllersDeduplicatesSameQualifiedTarget(t *testing.T) {
	t.Parallel()
	controller := fluxTargetTestController("GitRepository", "app-source")
	source := fluxTargetTestSource("repo-app", "flux-system", "app-source")
	bindings := source["flux_git_repository_bindings"].([]map[string]any)
	source["flux_git_repository_bindings"] = append(bindings, map[string]any{"namespace": "flux-system", "name": "app-source"})
	tally := bindFluxControllersToCrossRepoTargets([]map[string]any{controller}, []map[string]any{source})
	if tally.Linked != 1 || StringVal(controller, "flux_target_repo_id") != "repo-app" {
		t.Fatalf("controller/tally = %#v/%#v", controller, tally)
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
		"namespace":       "flux-system",
	}
}

func fluxTargetTestSource(targetRepoID string, namespace string, name string) map[string]any {
	return map[string]any{
		"relationship_type":            "DEPLOYS_FROM",
		"source_id":                    "repo-deploy",
		"target_id":                    targetRepoID,
		"flux_git_repository_bindings": []map[string]any{{"namespace": namespace, "name": name}},
	}
}
