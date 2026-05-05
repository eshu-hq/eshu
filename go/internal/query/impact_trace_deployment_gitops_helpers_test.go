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

	got := selectRelevantDeploymentSourceControllers("sample-service-api", deploymentSources, entities)
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

func TestCollectDeploymentSourceK8sResourcesIncludesRootScopedAssetsWithAttribution(t *testing.T) {
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
				"qualified_name": "dashboard/request-latency",
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
	if len(got) != 4 {
		t.Fatalf("len(collectDeploymentSourceK8sResources()) = %d, want 4", len(got))
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
		StringVal(got[3], "kind"),
	}
	wantKinds := []string{"ConfigMap", "DashboardAsset", "Deployment", "XIRSARole"}
	if !reflect.DeepEqual(resourceKinds, wantKinds) {
		t.Fatalf("resource kinds = %#v, want %#v", resourceKinds, wantKinds)
	}
}
