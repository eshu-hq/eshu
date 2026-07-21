// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

func TestSelectRelevantDeploymentSourceControllersFiltersToServiceScopedArgoCDRoots(t *testing.T) {
	t.Parallel()

	deploymentSources := []map[string]any{
		{
			"repo_id":   "repo-helm",
			"repo_name": "helm-charts",
		},
	}

	entities := []EntityContent{
		{
			EntityID:     "app-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"source_path": "services/sample-service-api/overlays/prod",
			},
		},
		{
			EntityID:     "appset-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/argocd/appset.yaml",
			EntityType:   "ArgoCDApplicationSet",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"generator_source_paths": "services/*/config.yaml",
				"template_source_paths":  "services/sample-service-api/overlays/prod",
			},
		},
		{
			EntityID:     "payments-app",
			RepoID:       "repo-helm",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
		{
			EntityID:     "other-repo-app",
			RepoID:       "repo-other",
			RelativePath: "services/sample-service-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"source_path": "services/sample-service-api/overlays/prod",
			},
		},
	}

	got := selectRelevantDeploymentSourceControllers("sample-service-api", "", 0, deploymentSources, entities)
	if len(got) != 2 {
		t.Fatalf("len(selectRelevantDeploymentSourceControllers()) = %d, want 2", len(got))
	}

	gotIDs := []string{StringVal(got[0], "entity_id"), StringVal(got[1], "entity_id")}
	wantIDs := []string{"app-1", "appset-1"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("selected controller ids = %#v, want %#v", gotIDs, wantIDs)
	}

	if got, want := StringVal(got[0], "controller_kind"), "argocd_application"; got != want {
		t.Fatalf("controllers[0].controller_kind = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "source_root"), "services/sample-service-api/overlays/prod"; got != want {
		t.Fatalf("controllers[0].source_root = %q, want %q", got, want)
	}
	if got, want := stringSliceMapValue(got[1], "source_roots"), []string{"services/sample-service-api/overlays/prod"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllers[1].source_roots = %#v, want %#v", got, want)
	}
	if got, want := stringSliceMapValue(got[1], "discovery_roots"), []string{"services"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllers[1].discovery_roots = %#v, want %#v", got, want)
	}
}

func TestSelectRelevantDeploymentSourceControllersDoesNotFallBackToUnrelatedControllers(t *testing.T) {
	t.Parallel()

	deploymentSources := []map[string]any{{
		"repo_id":   "repo-helm",
		"repo_name": "helm-charts",
	}}
	entities := []EntityContent{{
		EntityID:     "payments-app",
		RepoID:       "repo-helm",
		RelativePath: "services/payments-api/argocd/application.yaml",
		EntityType:   "ArgoCDApplication",
		EntityName:   "payments-api",
		Metadata: map[string]any{
			"source_path": "services/payments-api/overlays/prod",
		},
	}}

	got := selectRelevantDeploymentSourceControllers("orders-api", "", 0, deploymentSources, entities)
	if len(got) != 0 {
		t.Fatalf("selected unrelated controllers = %#v, want none", got)
	}
}

// TestSelectRelevantDeploymentSourceControllersTrustsSoleWorkloadOwnRepoController
// models the deployable-config fixture (#5471 defect A): the traced
// workload's own repo (workloadRepoID) DEFINES exactly one workload
// (ownRepoWorkloadCount=1 -- deployable-config itself), and its lone GitOps
// controller's own entity name/path names the DEPLOYED APP
// ("deployable-source") rather than the config repo/workload being traced
// ("deployable-config") -- the service-name-token match can never pass for
// it. Because the repo defines only one workload, that workload IS the one
// being traced, so its controller must still be selected.
func TestSelectRelevantDeploymentSourceControllersTrustsSoleWorkloadOwnRepoController(t *testing.T) {
	t.Parallel()

	// deploymentSources names a DIFFERENT repo (the application's
	// source-code repo), mirroring how deployable-config's canonical
	// DEPLOYMENT_SOURCE graph edge points at deployable-source, not at
	// deployable-config's own repo. It carries no controllers at all.
	deploymentSources := []map[string]any{{
		"repo_id":   "repository:deployable-source",
		"repo_name": "deployable-source",
	}}
	entities := []EntityContent{{
		EntityID:     "argocd-app-1",
		RepoID:       "repository:deployable-config",
		RelativePath: "application.yaml",
		EntityType:   "ArgoCDApplication",
		EntityName:   "deployable-source",
		Metadata: map[string]any{
			"source_repo": "https://github.com/acme/deployable-source",
			"source_path": "k8s",
		},
	}}

	got := selectRelevantDeploymentSourceControllers("deployable-config", "repository:deployable-config", 1, deploymentSources, entities)
	if len(got) != 1 {
		t.Fatalf("len(selectRelevantDeploymentSourceControllers()) = %d, want 1 (sole-workload own-repo controller): %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "entity_id"), "argocd-app-1"; got != want {
		t.Fatalf("selected controller id = %q, want %q", got, want)
	}
}

// TestSelectRelevantDeploymentSourceControllersRequiresTokenMatchWhenOwnRepoDefinesMultipleWorkloadsAndBothControllersIndexed
// is a P0 regression test for #5471 review round 2: a GitOps config repo
// used as the traced workload's own repo can be an app-of-apps monorepo
// DEFINING MANY workloads, each with its own ArgoCD Application (the
// repo-helm fixture's shape, reused here as workloadRepoID;
// ownRepoWorkloadCount=2 reflects that both sample-service-api and
// payments-api are DEFINES'd by this repo). Tracing "sample-service-api"
// must select ONLY its own controller and must NEVER pull "payments-api"'s
// controller into the response merely because both live in the traced
// workload's own repo -- that would leak payments-api's
// k8s_resources/image_refs into sample-service-api's deployment-truth-tier
// evidence.
func TestSelectRelevantDeploymentSourceControllersRequiresTokenMatchWhenOwnRepoDefinesMultipleWorkloadsAndBothControllersIndexed(t *testing.T) {
	t.Parallel()

	entities := []EntityContent{
		{
			EntityID:     "app-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"source_path": "services/sample-service-api/overlays/prod",
			},
		},
		{
			EntityID:     "payments-app",
			RepoID:       "repo-helm",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
	}

	got := selectRelevantDeploymentSourceControllers("sample-service-api", "repo-helm", 2, nil, entities)
	if len(got) != 1 {
		t.Fatalf("len(selectRelevantDeploymentSourceControllers()) = %d, want 1 (own repo defines 2 workloads -> token match required): %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "entity_id"), "app-1"; got != want {
		t.Fatalf("selected controller id = %q, want %q (payments-api's controller must not leak)", got, want)
	}
}

// TestSelectRelevantDeploymentSourceControllersRequiresTokenMatchWhenOwnRepoDefinesMultipleWorkloadsButOnlyOtherControllerIndexed
// is the P0 regression test for #5471 review round 3: the round-2 fix gated
// own-repo trust on countControllerEntitiesInRepo == 1, which conflated
// controller-entity uniqueness with workload OWNERSHIP uniqueness. A shared
// app-of-apps repo can DEFINE workloads A ("sample-service-api") and B
// ("payments-api") while ONLY B's controller has been indexed so far --
// ordinary partial discovery, nothing requires both workloads' controllers to
// be indexed atomically. Tracing A against that repo must still see
// ownRepoWorkloadCount=2 (the repo defines two workloads, independent of how
// many controller entities are indexed) and fall back to the service-name
// token match, which correctly rejects B's controller for A's trace. RED
// under a controller-count==1 gate (which would wrongly trust B's
// controller solely because it is the only one indexed); GREEN under the
// workload-count gate.
func TestSelectRelevantDeploymentSourceControllersRequiresTokenMatchWhenOwnRepoDefinesMultipleWorkloadsButOnlyOtherControllerIndexed(t *testing.T) {
	t.Parallel()

	// Only payments-api's controller is indexed; sample-service-api's own
	// controller has not been discovered/indexed yet.
	entities := []EntityContent{
		{
			EntityID:     "payments-app",
			RepoID:       "repo-helm",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
	}

	got := selectRelevantDeploymentSourceControllers("sample-service-api", "repo-helm", 2, nil, entities)
	if len(got) != 0 {
		t.Fatalf("selectRelevantDeploymentSourceControllers() = %#v, want none (payments-api's controller must not leak into sample-service-api's trace merely because it is the only controller indexed in their shared repo)", got)
	}
}

func TestSelectRelevantDeploymentSourceControllersRejectsShortNameCollisions(t *testing.T) {
	t.Parallel()

	deploymentSources := []map[string]any{{
		"repo_id":   "repo-helm",
		"repo_name": "helm-charts",
	}}
	entities := []EntityContent{
		{
			EntityID:     "payments-api-app",
			RepoID:       "repo-helm",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
		{
			EntityID:     "api-app",
			RepoID:       "repo-helm",
			RelativePath: "services/api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "api-controller",
			Metadata: map[string]any{
				"source_path": "services/api/overlays/prod",
			},
		},
	}

	got := selectRelevantDeploymentSourceControllers("api", "", 0, deploymentSources, entities)
	if len(got) != 1 {
		t.Fatalf("len(selectRelevantDeploymentSourceControllers()) = %d, want 1: %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "entity_id"), "api-app"; got != want {
		t.Fatalf("selected controller id = %q, want %q", got, want)
	}
}

func TestCollectDeploymentSourceK8sResourcesIncludesRootScopedResourcesWithAttribution(t *testing.T) {
	t.Parallel()

	controllerEntities := []map[string]any{
		{
			"entity_id":       "app-1",
			"entity_name":     "sample-service-api",
			"controller_kind": "argocd_application",
			"repo_id":         "repo-helm",
			"relative_path":   "services/sample-service-api/argocd/application.yaml",
			"source_root":     "services/sample-service-api/overlays/prod",
			"source_roots":    []string{"services/sample-service-api/overlays/prod"},
		},
	}

	entities := []EntityContent{
		{
			EntityID:     "deploy-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/overlays/prod/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"qualified_name":   "samples/Deployment/sample-service-api",
				"container_images": []any{"ghcr.io/acme/sample-service-api:1.2.3"},
			},
		},
		{
			EntityID:     "config-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/overlays/prod/configmap.yaml",
			EntityType:   "K8sResource",
			EntityName:   "sample-service-api-config",
			Metadata: map[string]any{
				"kind":           "ConfigMap",
				"qualified_name": "samples/ConfigMap/sample-service-api-config",
			},
		},
		{
			EntityID:     "irsa-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/overlays/prod/irsa.yaml",
			EntityType:   "K8sResource",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"kind":           "XIRSARole",
				"qualified_name": "samples/XIRSARole/sample-service-api",
			},
		},
		{
			EntityID:     "dashboard-1",
			RepoID:       "repo-helm",
			RelativePath: "services/sample-service-api/overlays/prod/dashboards/request-latency.json",
			EntityType:   "DashboardAsset",
			EntityName:   "request-latency",
			Metadata: map[string]any{
				"qualified_name":   "dashboard/request-latency",
				"container_images": []any{"ghcr.io/acme/dashboard-renderer:9.9.9"},
			},
		},
		{
			EntityID:     "other-1",
			RepoID:       "repo-helm",
			RelativePath: "services/payments-api/overlays/prod/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"kind":           "Deployment",
				"qualified_name": "payments/Deployment/payments-api",
			},
		},
	}

	got, imageRefs := collectDeploymentSourceK8sResources(controllerEntities, entities)
	if len(got) != 3 {
		t.Fatalf("len(collectDeploymentSourceK8sResources()) = %d, want 3", len(got))
	}
	if got, want := imageRefs, []string{"ghcr.io/acme/sample-service-api:1.2.3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("imageRefs = %#v, want %#v", got, want)
	}

	first := got[0]
	if got, want := StringVal(first, "repo_id"), "repo-helm"; got != want {
		t.Fatalf("resources[0].repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(first, "source_root"), "services/sample-service-api/overlays/prod"; got != want {
		t.Fatalf("resources[0].source_root = %q, want %q", got, want)
	}
	if got, want := StringVal(first, "controller_kind"), "argocd_application"; got != want {
		t.Fatalf("resources[0].controller_kind = %q, want %q", got, want)
	}

	resourceKinds := []string{
		StringVal(got[0], "kind"),
		StringVal(got[1], "kind"),
		StringVal(got[2], "kind"),
	}
	wantKinds := []string{"ConfigMap", "Deployment", "XIRSARole"}
	if !reflect.DeepEqual(resourceKinds, wantKinds) {
		t.Fatalf("resource kinds = %#v, want %#v", resourceKinds, wantKinds)
	}
}
