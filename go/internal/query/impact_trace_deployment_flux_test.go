// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

type fluxCrossRepoContentStore struct {
	fakePortContentStore
	entitiesByRepo map[string][]EntityContent
}

func (s fluxCrossRepoContentStore) ListRepoEntities(_ context.Context, repoID string, limit int) ([]EntityContent, error) {
	rows := append([]EntityContent(nil), s.entitiesByRepo[repoID]...)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// TestBuildDeploymentSourceControllerEntityFluxKustomizationUsesSourcePathAsIs
// proves FluxKustomization needs no special-casing: the Flux parser already
// emits spec.path under the same "source_path" metadata key ArgoCD uses, so
// buildDeploymentSourceControllerEntity's existing deploymentTraceSourceRoots
// call picks it up unmodified (issue #5483 C2).
func TestBuildDeploymentSourceControllerEntityFluxKustomizationUsesSourcePathAsIs(t *testing.T) {
	t.Parallel()

	entity := EntityContent{
		EntityID:     "flux-kustomization-1",
		RepoID:       "repo-deploy",
		RelativePath: "clusters/prod/apps-kustomization.yaml",
		EntityType:   "FluxKustomization",
		EntityName:   "payments-app",
		Metadata: map[string]any{
			"namespace":            "flux-system",
			"source_path":          "apps/payments/overlays/prod",
			"source_ref_kind":      "GitRepository",
			"source_ref_name":      "app-source",
			"source_ref_namespace": "source-system",
		},
	}

	controller, ok := buildDeploymentSourceControllerEntity(entity)
	if !ok {
		t.Fatal("buildDeploymentSourceControllerEntity() ok = false, want true for a registered FluxKustomization entity type")
	}
	if got, want := controller["controller_kind"], "flux_kustomization"; got != want {
		t.Fatalf("controller_kind = %#v, want %#v", got, want)
	}
	if got, want := controller["namespace"], "flux-system"; got != want {
		t.Fatalf("namespace = %#v, want %#v", got, want)
	}
	if got, want := controller["source_ref_namespace"], "source-system"; got != want {
		t.Fatalf("source_ref_namespace = %#v, want %#v", got, want)
	}
	roots, _ := controller["source_roots"].([]string)
	if len(roots) != 1 || roots[0] != "apps/payments/overlays/prod" {
		t.Fatalf("source_roots = %#v, want [apps/payments/overlays/prod]", roots)
	}
}

// TestBuildDeploymentSourceControllerEntityFluxHelmReleaseChartAsPathRoot
// covers the FluxHelmRelease chart-vs-path split (issue #5483 C2): chart is a
// PATH root only when source_ref_kind is GitRepository or Bucket, per the
// Flux HelmRelease API.
func TestBuildDeploymentSourceControllerEntityFluxHelmReleaseChartAsPathRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceRefKind string
		wantRoots     []string
	}{
		{
			name:          "GitRepository source treats chart as a path root",
			sourceRefKind: "GitRepository",
			wantRoots:     []string{"charts/podinfo"},
		},
		{
			name:          "Bucket source treats chart as a path root",
			sourceRefKind: "Bucket",
			wantRoots:     []string{"charts/podinfo"},
		},
		{
			name:          "HelmRepository source does NOT treat chart as a path root (it is a chart name)",
			sourceRefKind: "HelmRepository",
			wantRoots:     nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entity := EntityContent{
				EntityID:     "flux-helmrelease-1",
				RepoID:       "repo-deploy",
				RelativePath: "clusters/prod/podinfo-helmrelease.yaml",
				EntityType:   "FluxHelmRelease",
				EntityName:   "podinfo",
				Metadata: map[string]any{
					"chart":           "charts/podinfo",
					"source_ref_kind": tt.sourceRefKind,
					"source_ref_name": "app-source",
				},
			}

			controller, ok := buildDeploymentSourceControllerEntity(entity)
			if !ok {
				t.Fatal("buildDeploymentSourceControllerEntity() ok = false, want true for a registered FluxHelmRelease entity type")
			}
			roots, _ := controller["source_roots"].([]string)
			if len(roots) != len(tt.wantRoots) {
				t.Fatalf("source_roots = %#v, want %#v", roots, tt.wantRoots)
			}
			for i, want := range tt.wantRoots {
				if roots[i] != want {
					t.Fatalf("source_roots = %#v, want %#v", roots, tt.wantRoots)
				}
			}
		})
	}
}

// TestFetchControllerEntitiesReturnsFluxControllersFromDeploymentSources is
// the query-truth proof for #5483 C2's deployment-trace consumer: once a
// FluxGitRepository spec.url resolves a DEPLOYS_FROM edge into
// deployment_sources (the reducer/graph side proven in
// go/internal/storage/postgres/ingestion_flux_cross_repo_evidence_integration_test.go),
// the trace's controller-fetch step must surface the Flux controllers living
// in that resolved repository, mirroring the existing ArgoCD proof
// (TestFetchControllerEntitiesReturnsArgoCDControllersFromDeploymentSources).
func TestFetchControllerEntitiesReturnsFluxControllersFromDeploymentSources(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"flux-kustomization-1", "repo-deploy", "clusters/prod/apps-kustomization.yaml", "FluxKustomization", "payments-app",
					int64(1), int64(20), "yaml", "kind: Kustomization",
					[]byte(`{"source_path":"apps/payments/overlays/prod","source_ref_kind":"GitRepository","source_ref_name":"app-source"}`),
				},
				{
					"flux-helmrelease-1", "repo-deploy", "clusters/prod/podinfo-helmrelease.yaml", "FluxHelmRelease", "podinfo",
					int64(1), int64(15), "yaml", "kind: HelmRelease",
					[]byte(`{"chart":"charts/podinfo","source_ref_kind":"GitRepository","source_ref_name":"app-source"}`),
				},
				{
					"k8s-1", "repo-deploy", "apps/payments/overlays/prod/deployment.yaml", "K8sResource", "payments-api",
					int64(1), int64(10), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment"}`),
				},
			},
		},
	})

	handler := &ImpactHandler{Content: NewContentReader(db)}
	deploymentSources := []map[string]any{
		{
			"repo_id":   "repo-deploy",
			"repo_name": "payments-deploy",
		},
	}

	got, err := handler.fetchControllerEntities(context.Background(), deploymentSources)
	if err != nil {
		t.Fatalf("fetchControllerEntities() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchControllerEntities()) = %d, want 2: %#v", len(got), got)
	}
	controllersByType := make(map[string]map[string]any, len(got))
	for _, controller := range got {
		entityType, _ := controller["entity_type"].(string)
		controllersByType[entityType] = controller
	}
	kustomization, ok := controllersByType["FluxKustomization"]
	if !ok {
		t.Fatalf("fetchControllerEntities() missing FluxKustomization: %#v", got)
	}
	if got, want := kustomization["controller_kind"], "flux_kustomization"; got != want {
		t.Fatalf("FluxKustomization controller_kind = %#v, want %#v", got, want)
	}

	helmRelease, ok := controllersByType["FluxHelmRelease"]
	if !ok {
		t.Fatalf("fetchControllerEntities() missing FluxHelmRelease: %#v", got)
	}
	if got, want := helmRelease["controller_kind"], "flux_helm_release"; got != want {
		t.Fatalf("FluxHelmRelease controller_kind = %#v, want %#v", got, want)
	}
	roots, _ := helmRelease["source_roots"].([]string)
	if len(roots) != 1 || roots[0] != "charts/podinfo" {
		t.Fatalf("FluxHelmRelease source_roots = %#v, want [charts/podinfo] (GitRepository source treats chart as a path)", roots)
	}
}

func TestFetchDeploymentSourceGitOpsResultAttributesCrossRepoFluxTargetResources(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Content: fluxCrossRepoContentStore{entitiesByRepo: map[string][]EntityContent{
		"repo-deploy": {{
			EntityID:     "flux-kustomization:payments",
			RepoID:       "repo-deploy",
			EntityType:   "FluxKustomization",
			EntityName:   "payments-api",
			RelativePath: "clusters/prod/payments.yaml",
			Metadata: map[string]any{
				"namespace":       "flux-system",
				"source_path":     "apps/payments",
				"source_ref_kind": "GitRepository",
				"source_ref_name": "app-source",
			},
		}},
		"repo-app": {{
			EntityID:     "k8s:payments-deployment",
			RepoID:       "repo-app",
			EntityType:   "K8sResource",
			EntityName:   "payments-api",
			RelativePath: "apps/payments/deployment.yaml",
			Metadata:     map[string]any{"kind": "Deployment"},
		}},
	}}}

	result, err := handler.fetchDeploymentSourceGitOpsResult(t.Context(), "payments-api", "repo-app", []map[string]any{{
		"repo_id":                      "repo-deploy",
		"relationship_type":            "DEPLOYS_FROM",
		"source_id":                    "repo-deploy",
		"target_id":                    "repo-app",
		"flux_git_repository_bindings": []map[string]any{{"namespace": "flux-system", "name": "app-source"}},
	}})
	if err != nil {
		t.Fatalf("fetchDeploymentSourceGitOpsResult() error = %v", err)
	}
	if got, want := len(result.k8sResources), 1; got != want {
		t.Fatalf("len(k8sResources) = %d, want %d; result = %#v", got, want, result)
	}
	if got, want := StringVal(result.k8sResources[0], "repo_id"), "repo-app"; got != want {
		t.Fatalf("k8s resource repo_id = %q, want %q", got, want)
	}
}
