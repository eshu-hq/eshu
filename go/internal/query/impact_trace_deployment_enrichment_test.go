// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildDeploymentTraceResponseUsesCanonicalServiceNameAndDrilldowns(t *testing.T) {
	t.Parallel()

	got := buildDeploymentTraceResponse("workload:service-edge-api", map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "service",
		"repo_id":   "repo-service-edge-api",
		"repo_name": "service-edge-api",
	})

	if got["service_name"] != "service-edge-api" {
		t.Fatalf("service_name = %#v, want %q", got["service_name"], "service-edge-api")
	}

	drilldowns, ok := got["drilldowns"].(map[string]any)
	if !ok {
		t.Fatalf("drilldowns type = %T, want map[string]any", got["drilldowns"])
	}
	if got, want := drilldowns["service_context_path"], "/api/v0/services/service-edge-api/context"; got != want {
		t.Fatalf("drilldowns.service_context_path = %#v, want %#v", got, want)
	}
	if got, want := drilldowns["service_story_path"], "/api/v0/services/service-edge-api/story"; got != want {
		t.Fatalf("drilldowns.service_story_path = %#v, want %#v", got, want)
	}
}

func TestTraceEnrichmentOptionsDirectOnlySkipsIndirectEvidence(t *testing.T) {
	t.Parallel()

	options := traceEnrichmentOptions(traceDeploymentChainRequest{
		ServiceName:               "payments-api",
		DirectOnly:                true,
		IncludeRelatedModuleUsage: true,
	})

	if options.includeConsumers {
		t.Fatal("includeConsumers = true, want false when direct_only is enabled")
	}
	if options.includeProvisioningChains {
		t.Fatal("includeProvisioningChains = true, want false when direct_only is enabled")
	}
}

func TestTraceEnrichmentOptionsHonorsRelatedModuleUsageFlag(t *testing.T) {
	t.Parallel()

	options := traceEnrichmentOptions(traceDeploymentChainRequest{
		ServiceName:               "payments-api",
		IncludeRelatedModuleUsage: true,
	})

	if !options.includeConsumers {
		t.Fatal("includeConsumers = false, want true for non-direct trace")
	}
	if !options.includeProvisioningChains {
		t.Fatal("includeProvisioningChains = false, want true when related module usage is requested")
	}
}

func TestBoundedIndirectEvidenceHostnamesTrimsDeduplicatesAndCaps(t *testing.T) {
	t.Parallel()

	got := boundedIndirectEvidenceHostnamesForService([]string{
		"",
		"api.qa.example.test",
		" api.qa.example.test ",
		"api.prod.example.test",
		"api.stage.example.test",
		"api.dev.example.test",
		"api.extra.example.test",
	}, "")

	want := []string{
		"api.dev.example.test",
		"api.prod.example.test",
		"api.qa.example.test",
		"api.stage.example.test",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("boundedIndirectEvidenceHostnamesForService() = %#v, want %#v", got, want)
	}
}

func TestBoundedIndirectEvidenceHostnamesPrefersServiceOwnedHosts(t *testing.T) {
	t.Parallel()

	got := boundedIndirectEvidenceHostnamesForService([]string{
		"api.vendor.example.test",
		"docs.vendor.example.test",
		"checkout.qa.example.test",
		"metrics.vendor.example.test",
		"checkout.prod.example.test",
	}, "sample-checkout-api")

	want := []string{
		"checkout.prod.example.test",
		"checkout.qa.example.test",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("boundedIndirectEvidenceHostnamesForService() = %#v, want %#v", got, want)
	}
}

func TestBuildDeploymentTraceResponseNarratesTypedControllerProvenance(t *testing.T) {
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
				"entity_id":       "argocd-app-1",
				"entity_type":     "ArgoCDApplication",
				"entity_name":     "payments-app",
				"controller_kind": "argocd_application",
				"repo_id":         "repo-deploy",
				"relative_path":   "argocd/payments.yaml",
				"source_repo":     "https://github.com/myorg/payments-deploy.git",
				"source_path":     "deploy/overlays/prod",
				"dest_server":     "https://kubernetes.default.svc",
				"dest_namespace":  "payments",
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)
	story, ok := got["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", got["story"])
	}
	if story == "" {
		t.Fatal("story is empty, want typed provenance narrative")
	}
	if !strings.Contains(story, "payments-app") {
		t.Fatalf("story = %q, want controller entity name", story)
	}
	if !strings.Contains(story, "argocd_application") {
		t.Fatalf("story = %q, want controller kind", story)
	}
	if !strings.Contains(story, "payments-deploy") {
		t.Fatalf("story = %q, want deployment source repo", story)
	}
}

func TestBuildDeploymentTraceResponseIncludesServiceEvidenceConsumersAndProvisioningChains(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:sample-service-api:qa",
				"platform_name": "ecs-qa",
				"platform_kind": "ecs_service",
				"environment":   "qa",
			},
			{
				"instance_id":   "workload-instance:sample-service-api:production",
				"platform_name": "eks-prod",
				"platform_kind": "argocd_applicationset",
				"environment":   "production",
			},
		},
		"hostnames": []map[string]any{
			{"hostname": "sample-service-api.qa.example.test", "environment": "qa"},
			{"hostname": "sample-service-api.production.example.test", "environment": "production"},
		},
		"entrypoints": []map[string]any{
			{"type": "hostname", "target": "sample-service-api.qa.example.test", "environment": "qa", "visibility": "public"},
		},
		"network_paths": []map[string]any{
			{"path_type": "hostname_to_runtime", "from": "sample-service-api.qa.example.test", "to": "eks-qa", "environment": "qa"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 2,
			"api_versions":   []string{"v3"},
			"docs_routes":    []string{"/_specs"},
			"spec_files":     []string{"specs/index.yaml"},
		},
		"dependents": []map[string]any{
			{"repository": "deployment-helm", "repo_id": "repo-helm", "relationship_types": []string{"DEPLOYS_FROM"}},
		},
		"consumer_repositories": []map[string]any{
			{
				"repository":     "svc-saved-search",
				"repo_id":        "repo-consumer-1",
				"evidence_kinds": []string{"repository_reference", "hostname_reference"},
				"matched_values": []string{"sample-service-api", "sample-service-api.qa.example.test"},
				"sample_paths":   []string{"config/local.json"},
			},
		},
		"provisioning_source_chains": []map[string]any{
			{
				"repository": "helm-charts",
				"repo_id":    "repo-helm",
				"modules":    []string{"envoy_gateway", "irsa"},
			},
		},
		"documentation_overview": map[string]any{
			"spec_files":  []string{"specs/index.yaml"},
			"docs_routes": []string{"/_specs"},
		},
		"support_overview": map[string]any{
			"consumer_count":            1,
			"provisioning_chain_count":  1,
			"hostname_count":            2,
			"documented_endpoint_count": 2,
		},
	}

	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if _, ok := deploymentOverview["hostnames"]; !ok {
		t.Fatal("deployment_overview.hostnames missing, want service entrypoint evidence")
	}
	if _, ok := deploymentOverview["entrypoints"]; !ok {
		t.Fatal("deployment_overview.entrypoints missing, want typed service entrypoints")
	}
	if _, ok := deploymentOverview["api_surface"]; !ok {
		t.Fatal("deployment_overview.api_surface missing, want API evidence")
	}

	if _, ok := got["entrypoints"]; !ok {
		t.Fatal("entrypoints missing, want typed service entrypoints")
	}
	if _, ok := got["network_paths"]; !ok {
		t.Fatal("network_paths missing, want evidence-backed entrypoint routing")
	}
	if _, ok := got["dependents"]; !ok {
		t.Fatal("dependents missing, want graph-derived dependent repositories")
	}
	if _, ok := got["consumer_repositories"]; !ok {
		t.Fatal("consumer_repositories missing, want query-time service consumer evidence")
	}
	if _, ok := got["provisioning_source_chains"]; !ok {
		t.Fatal("provisioning_source_chains missing, want IaC chain evidence")
	}
	if _, ok := got["documentation_overview"]; !ok {
		t.Fatal("documentation_overview missing, want service documentation summary")
	}
	if _, ok := got["support_overview"]; !ok {
		t.Fatal("support_overview missing, want service support summary")
	}
}

func TestBuildDeploymentTraceResponseRecognizesGitOpsFromReadModelEvidence(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:sample-service-api:prod",
				"platform_name": "prod",
				"platform_kind": "kubernetes",
				"environment":   "prod",
				"platforms": []map[string]any{
					{
						"platform_name": "prod",
						"platform_kind": "kubernetes",
					},
					{
						"platform_name": "runtime-ecs",
						"platform_kind": "ecs",
					},
				},
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-gitops",
				"repo_name":  "delivery-gitops",
				"confidence": 0.99,
				"reason":     "argocd_applicationset_deploy_source",
			},
		},
		"deployment_evidence": map[string]any{
			"tool_families": []string{"argocd", "github_actions", "helm", "kustomize"},
			"artifacts": []map[string]any{
				{
					"family":        "argocd",
					"evidence_type": "argocd_applicationset_deploy_source",
					"resolved_id":   "resolved-gitops",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	gitopsOverview, ok := got["gitops_overview"].(map[string]any)
	if !ok {
		t.Fatalf("gitops_overview type = %T, want map[string]any", got["gitops_overview"])
	}
	if gitopsOverview["enabled"] != true {
		t.Fatalf("gitops_overview.enabled = %#v, want true", gitopsOverview["enabled"])
	}
	if !slices.Contains(StringSliceVal(gitopsOverview, "tool_families"), "argocd") {
		t.Fatalf("gitops_overview.tool_families = %#v, want argocd", gitopsOverview["tool_families"])
	}

	controllerOverview, ok := got["controller_overview"].(map[string]any)
	if !ok {
		t.Fatalf("controller_overview type = %T, want map[string]any", got["controller_overview"])
	}
	if !slices.Contains(StringSliceVal(controllerOverview, "controller_kinds"), "argocd") {
		t.Fatalf("controller_overview.controller_kinds = %#v, want argocd", controllerOverview["controller_kinds"])
	}

	controllerDrivenPaths, ok := got["controller_driven_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_driven_paths type = %T, want []map[string]any", got["controller_driven_paths"])
	}
	if len(controllerDrivenPaths) != 2 {
		t.Fatalf("len(controller_driven_paths) = %d, want 2", len(controllerDrivenPaths))
	}
	pathsByTarget := make(map[string]map[string]any, len(controllerDrivenPaths))
	for _, path := range controllerDrivenPaths {
		pathsByTarget[StringVal(path, "observed_target")] = path
	}
	if gotKind, wantKind := StringVal(pathsByTarget["prod"], "controller_kind"), "kubernetes"; gotKind != wantKind {
		t.Fatalf("controller_driven_paths[prod].controller_kind = %q, want %q", gotKind, wantKind)
	}
	if gotKind, wantKind := StringVal(pathsByTarget["runtime-ecs"], "controller_kind"), "ecs"; gotKind != wantKind {
		t.Fatalf("controller_driven_paths[runtime-ecs].controller_kind = %q, want %q", gotKind, wantKind)
	}
}
