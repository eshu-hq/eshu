// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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

	got := impacttrace.BuildDeploymentTraceResponse("payments-api", ctx, map[string]any{})
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

	// In-memory entities: impact/ tests cannot build the root ContentReader
	// (package-query import would cycle through family_impact_shim.go), and
	// the SQL decoding layer stays covered by the root content_reader tests.
	// See #6060.
	handler := &ImpactHandler{Content: &querytestutil.FakePortContentStore{
		Entities: []querycontract.EntityContent{
			{
				EntityID: "argocd-app-1", RepoID: "repo-deploy", RelativePath: "argocd/payments.yaml",
				EntityType: "ArgoCDApplication", EntityName: "payments-app",
				StartLine: 1, EndLine: 20, Language: "yaml", SourceCache: "kind: Application",
				Metadata: map[string]any{
					"source_repo": "https://github.com/myorg/payments-deploy.git", "source_path": "deploy/overlays/prod",
					"dest_server": "https://kubernetes.default.svc", "dest_namespace": "payments",
				},
			},
			{
				EntityID: "argocd-appset-1", RepoID: "repo-deploy", RelativePath: "argocd/appset.yaml",
				EntityType: "ArgoCDApplicationSet", EntityName: "payments-appset",
				StartLine: 1, EndLine: 30, Language: "yaml", SourceCache: "kind: ApplicationSet",
				Metadata: map[string]any{
					"generator_source_repos": "https://github.com/myorg/platform-config.git",
					"template_source_repos":  "https://github.com/myorg/platform-runtime.git",
					"dest_server":            "https://kubernetes.default.svc", "dest_namespace": "payments",
				},
			},
			{
				EntityID: "k8s-1", RepoID: "repo-deploy", RelativePath: "deploy/service.yaml",
				EntityType: "K8sResource", EntityName: "payments-api",
				StartLine: 1, EndLine: 10, Language: "yaml", SourceCache: "kind: Service",
				Metadata: map[string]any{"kind": "Service"},
			},
		},
	}}
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
