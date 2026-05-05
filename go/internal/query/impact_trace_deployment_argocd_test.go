package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestBuildDeploymentTraceResponseIncludesControllerEntities(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "payments",
		"instances": []map[string]any{
			{
				"instance_id":   "inst-1",
				"platform_name": "payments-argocd",
				"platform_kind": "argocd_application",
				"environment":   "prod",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":   "repo-deploy",
				"repo_name": "payments-deploy",
			},
		},
		"controller_entities": []map[string]any{
			{
				"entity_id":     "argocd-app-1",
				"entity_type":   "ArgoCDApplication",
				"entity_name":   "payments-app",
				"repo_id":       "repo-deploy",
				"relative_path": "argocd/payments.yaml",
				"source_repo":   "https://github.com/myorg/payments-deploy.git",
				"source_path":   "deploy/overlays/prod",
				"dest_server":   "https://kubernetes.default.svc",
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)
	controllerOverview, ok := got["controller_overview"].(map[string]any)
	if !ok {
		t.Fatalf("controller_overview type = %T, want map[string]any", got["controller_overview"])
	}

	entities, ok := controllerOverview["entities"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_overview.entities type = %T, want []map[string]any", controllerOverview["entities"])
	}
	if len(entities) != 1 {
		t.Fatalf("len(controller_overview.entities) = %d, want 1", len(entities))
	}
	if gotValue, want := entities[0]["entity_name"], "payments-app"; gotValue != want {
		t.Fatalf("controller_overview.entities[0][entity_name] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := entities[0]["source_repo"], "https://github.com/myorg/payments-deploy.git"; gotValue != want {
		t.Fatalf("controller_overview.entities[0][source_repo] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := entities[0]["source_path"], "deploy/overlays/prod"; gotValue != want {
		t.Fatalf("controller_overview.entities[0][source_path] = %#v, want %#v", gotValue, want)
	}
}

func TestFetchControllerEntitiesReturnsArgoCDControllersFromDeploymentSources(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"argocd-app-1", "repo-deploy", "argocd/payments.yaml", "ArgoCDApplication", "payments-app",
					int64(1), int64(20), "yaml", "kind: Application", []byte(`{"source_repo":"https://github.com/myorg/payments-deploy.git","source_path":"deploy/overlays/prod","dest_server":"https://kubernetes.default.svc","dest_namespace":"payments"}`),
				},
				{
					"argocd-appset-1", "repo-deploy", "argocd/appset.yaml", "ArgoCDApplicationSet", "payments-appset",
					int64(1), int64(30), "yaml", "kind: ApplicationSet", []byte(`{"generator_source_repos":"https://github.com/myorg/platform-config.git","template_source_repos":"https://github.com/myorg/platform-runtime.git","dest_server":"https://kubernetes.default.svc","dest_namespace":"payments"}`),
				},
				{
					"k8s-1", "repo-deploy", "deploy/service.yaml", "K8sResource", "payments-api",
					int64(1), int64(10), "yaml", "kind: Service", []byte(`{"kind":"Service"}`),
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
		t.Fatalf("len(fetchControllerEntities()) = %d, want 2", len(got))
	}
	controllersByType := make(map[string]map[string]any, len(got))
	for _, controller := range got {
		entityType, _ := controller["entity_type"].(string)
		controllersByType[entityType] = controller
	}
	if _, ok := controllersByType["ArgoCDApplication"]; !ok {
		t.Fatalf("fetchControllerEntities() missing ArgoCDApplication: %#v", got)
	}
	if _, ok := controllersByType["ArgoCDApplicationSet"]; !ok {
		t.Fatalf("fetchControllerEntities() missing ArgoCDApplicationSet: %#v", got)
	}
	if controllersByType["ArgoCDApplication"]["source_repo"] == "" {
		t.Fatal("ArgoCDApplication source_repo is empty, want source repo")
	}
	if len(controllersByType["ArgoCDApplicationSet"]["template_source_repos"].([]string)) == 0 {
		t.Fatal("ArgoCDApplicationSet template_source_repos is empty, want template source repos")
	}
}
