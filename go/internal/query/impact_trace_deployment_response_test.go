// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
)

func TestBuildDeploymentTraceResponseIncludesArtifactBackedDeliveryPaths(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()

	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	deliveryPaths := mapSliceValue(got, "delivery_paths")
	if len(deliveryPaths) == 0 {
		t.Fatalf("delivery_paths is empty, want deployment evidence artifact paths")
	}
	var seenGitOps, seenTerraform bool
	for _, path := range deliveryPaths {
		switch StringVal(path, "resolved_id") {
		case "resolved-gitops":
			seenGitOps = true
			if got, want := StringVal(path, "type"), "deployment_evidence"; got != want {
				t.Fatalf("gitops delivery path type = %q, want %q", got, want)
			}
			if got, want := StringVal(path, "artifact_family"), "argocd"; got != want {
				t.Fatalf("gitops artifact_family = %q, want %q", got, want)
			}
		case "resolved-terraform":
			seenTerraform = true
			if got, want := StringVal(path, "relationship_type"), "PROVISIONS_DEPENDENCY_FOR"; got != want {
				t.Fatalf("terraform relationship_type = %q, want %q", got, want)
			}
		}
	}
	if !seenGitOps || !seenTerraform {
		t.Fatalf("delivery_paths = %#v, want gitops and terraform artifact paths", deliveryPaths)
	}
}

func TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["cloud_resources"] = []map[string]any{}
	ctx["uncorrelated_cloud_resources"] = []map[string]any{
		{
			"id":                   "cloud:ssm:sample-service-client-port",
			"name":                 "/configd/sample-service/client/port",
			"resource_type":        "ssm_parameter",
			"provider":             "aws",
			"missing_relationship": "workload_cloud_relationship",
			"candidate_status":     "uncorrelated",
		},
	}

	got := buildDeploymentTraceResponse("sample-service-api", ctx)
	if cloudResources := mapSliceValue(got, "cloud_resources"); len(cloudResources) != 0 {
		t.Fatalf("cloud_resources = %#v, want no promoted attached resources", cloudResources)
	}
	candidates := mapSliceValue(got, "uncorrelated_cloud_resources")
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("uncorrelated_cloud_resources len = %d, want %d", got, want)
	}
	overview := mapValue(got, "deployment_overview")
	if got, want := IntVal(overview, "cloud_resource_count"), 0; got != want {
		t.Fatalf("deployment_overview.cloud_resource_count = %d, want %d", got, want)
	}
	if got, want := IntVal(overview, "uncorrelated_cloud_resource_count"), 1; got != want {
		t.Fatalf("deployment_overview.uncorrelated_cloud_resource_count = %d, want %d", got, want)
	}
	summary := mapValue(got, "deployment_fact_summary")
	missingEvidence := StringSliceVal(summary, "missing_evidence")
	if !stringSliceContains(missingEvidence, "workload_cloud_relationship_missing") {
		t.Fatalf("deployment_fact_summary.missing_evidence = %#v, want workload cloud relationship gap", missingEvidence)
	}
}

func TestBuildDeploymentTraceResponseSummarizesInstances(t *testing.T) {
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
				"platforms": []map[string]any{{
					"platform_id":   "platform:payments-argocd",
					"platform_name": "payments-argocd",
					"platform_kind": "argocd_application",
					"topology_edges": []map[string]any{{
						"relationship_type": "RUNS_ON",
						"source_id":         "inst-1",
						"target_id":         "platform:payments-argocd",
						"target_name":       "payments-argocd",
					}},
				}},
			},
			{
				"instance_id":   "inst-2",
				"platform_name": "payments-argocd",
				"platform_kind": "argocd_application",
				"environment":   "stage",
				"platforms": []map[string]any{{
					"platform_id":   "platform:payments-argocd",
					"platform_name": "payments-argocd",
					"platform_kind": "argocd_application",
					"topology_edges": []map[string]any{{
						"relationship_type": "RUNS_ON",
						"source_id":         "inst-2",
						"target_id":         "platform:payments-argocd",
						"target_name":       "payments-argocd",
					}},
				}},
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":           "repo-deploy",
				"repo_name":         "payments-deploy",
				"relationship_type": "DEPLOYMENT_SOURCE",
				"source_id":         "inst-1",
				"target_id":         "repo-deploy",
				"confidence":        0.98,
				"reason":            "Deployment manifests for workload instance live in deployment repository",
			},
		},
		"cloud_resources": []map[string]any{
			{
				"id":          "cloud-1",
				"name":        "payments-db",
				"kind":        "rds_instance",
				"provider":    "aws",
				"environment": "prod",
				"confidence":  0.91,
				"reason":      "Runtime instance uses backing database",
			},
		},
		"k8s_resources": []map[string]any{
			{
				"entity_id":        "k8s-0",
				"entity_name":      "payments-api",
				"kind":             "Service",
				"qualified_name":   "payments/Service/payments-api",
				"relative_path":    "deploy/service.yaml",
				"container_images": []string{},
			},
			{
				"entity_id":        "k8s-1",
				"entity_name":      "payments-api",
				"kind":             "Deployment",
				"qualified_name":   "payments/Deployment/payments-api",
				"relative_path":    "deploy/payments.yaml",
				"container_images": []string{"ghcr.io/acme/payments-api:1.2.3"},
			},
		},
		"image_refs": []string{"ghcr.io/acme/payments-api:1.2.3"},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)

	if got["service_name"] != "payments-api" {
		t.Fatalf("service_name = %#v, want %q", got["service_name"], "payments-api")
	}
	if got["story"] == "" {
		t.Fatal("story is empty, want narrative summary")
	}
	subject, ok := got["subject"].(map[string]any)
	if !ok {
		t.Fatalf("subject type = %T, want map[string]any", got["subject"])
	}
	if subject["name"] != "payments-api" {
		t.Fatalf("subject.name = %#v, want %q", subject["name"], "payments-api")
	}
	if got["repo_id"] != "repo-1" {
		t.Fatalf("repo_id = %#v, want %q", got["repo_id"], "repo-1")
	}
	if got["repo_name"] != "payments" {
		t.Fatalf("repo_name = %#v, want %q", got["repo_name"], "payments")
	}

	overview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if gotCount, want := overview["instance_count"], 2; gotCount != want {
		t.Fatalf("deployment_overview.instance_count = %#v, want %#v", gotCount, want)
	}
	if gotCount, want := overview["environment_count"], 2; gotCount != want {
		t.Fatalf("deployment_overview.environment_count = %#v, want %#v", gotCount, want)
	}
	if gotCount, want := overview["platform_count"], 1; gotCount != want {
		t.Fatalf("deployment_overview.platform_count = %#v, want %#v", gotCount, want)
	}

	platforms, ok := overview["platforms"].([]string)
	if !ok {
		t.Fatalf("deployment_overview.platforms type = %T, want []string", overview["platforms"])
	}
	if len(platforms) != 1 || platforms[0] != "payments-argocd" {
		t.Fatalf("deployment_overview.platforms = %#v, want [payments-argocd]", platforms)
	}

	kinds, ok := overview["platform_kinds"].([]string)
	if !ok {
		t.Fatalf("deployment_overview.platform_kinds type = %T, want []string", overview["platform_kinds"])
	}
	if len(kinds) != 1 || kinds[0] != "argocd_application" {
		t.Fatalf("deployment_overview.platform_kinds = %#v, want [argocd_application]", kinds)
	}

	environments, ok := overview["environments"].([]string)
	if !ok {
		t.Fatalf("deployment_overview.environments type = %T, want []string", overview["environments"])
	}
	if len(environments) != 2 {
		t.Fatalf("deployment_overview.environments len = %d, want 2", len(environments))
	}

	storySections, ok := got["story_sections"].([]map[string]any)
	if !ok {
		t.Fatalf("story_sections type = %T, want []map[string]any", got["story_sections"])
	}
	if len(storySections) == 0 {
		t.Fatal("story_sections is empty, want grouped supporting context")
	}

	controllerOverview, ok := got["controller_overview"].(map[string]any)
	if !ok {
		t.Fatalf("controller_overview type = %T, want map[string]any", got["controller_overview"])
	}
	if controllerOverview["controller_count"] != 1 {
		t.Fatalf("controller_overview.controller_count = %#v, want 1", controllerOverview["controller_count"])
	}
	gitopsOverview, ok := got["gitops_overview"].(map[string]any)
	if !ok {
		t.Fatalf("gitops_overview type = %T, want map[string]any", got["gitops_overview"])
	}
	if gitopsOverview["enabled"] != true {
		t.Fatalf("gitops_overview.enabled = %#v, want true", gitopsOverview["enabled"])
	}

	runtimeOverview, ok := got["runtime_overview"].(map[string]any)
	if !ok {
		t.Fatalf("runtime_overview type = %T, want map[string]any", got["runtime_overview"])
	}
	if runtimeOverview["environment_count"] != 2 {
		t.Fatalf("runtime_overview.environment_count = %#v, want 2", runtimeOverview["environment_count"])
	}

	factSummary, ok := got["deployment_fact_summary"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_fact_summary type = %T, want map[string]any", got["deployment_fact_summary"])
	}
	if factSummary["has_repository"] != true {
		t.Fatalf("deployment_fact_summary.has_repository = %#v, want true", factSummary["has_repository"])
	}
	if factSummary["mapping_mode"] != "controller" {
		t.Fatalf("deployment_fact_summary.mapping_mode = %#v, want %q", factSummary["mapping_mode"], "controller")
	}
	if _, ok := factSummary["overall_confidence"]; !ok {
		t.Fatal("deployment_fact_summary.overall_confidence missing")
	}
	if factSummary["overall_confidence_reason"] != "materialized_runtime_instances" {
		t.Fatalf(
			"deployment_fact_summary.overall_confidence_reason = %#v, want %q",
			factSummary["overall_confidence_reason"],
			"materialized_runtime_instances",
		)
	}
	if factSummary["materialized_environment_count"] != 2 {
		t.Fatalf(
			"deployment_fact_summary.materialized_environment_count = %#v, want 2",
			factSummary["materialized_environment_count"],
		)
	}
	if factSummary["config_environment_count"] != 0 {
		t.Fatalf(
			"deployment_fact_summary.config_environment_count = %#v, want 0",
			factSummary["config_environment_count"],
		)
	}

	deploymentFacts, ok := got["deployment_facts"].([]map[string]any)
	if !ok {
		t.Fatalf("deployment_facts type = %T, want []map[string]any", got["deployment_facts"])
	}
	if len(deploymentFacts) < 3 {
		t.Fatalf("deployment_facts len = %d, want at least 3", len(deploymentFacts))
	}

	deploymentSources, ok := got["deployment_sources"].([]map[string]any)
	if !ok {
		t.Fatalf("deployment_sources type = %T, want []map[string]any", got["deployment_sources"])
	}
	if len(deploymentSources) != 1 {
		t.Fatalf("deployment_sources len = %d, want 1", len(deploymentSources))
	}

	cloudResources, ok := got["cloud_resources"].([]map[string]any)
	if !ok {
		t.Fatalf("cloud_resources type = %T, want []map[string]any", got["cloud_resources"])
	}
	if len(cloudResources) != 1 {
		t.Fatalf("cloud_resources len = %d, want 1", len(cloudResources))
	}

	k8sResources, ok := got["k8s_resources"].([]map[string]any)
	if !ok {
		t.Fatalf("k8s_resources type = %T, want []map[string]any", got["k8s_resources"])
	}
	if len(k8sResources) != 2 {
		t.Fatalf("k8s_resources len = %d, want 2", len(k8sResources))
	}

	imageRefs, ok := got["image_refs"].([]string)
	if !ok {
		t.Fatalf("image_refs type = %T, want []string", got["image_refs"])
	}
	if len(imageRefs) != 1 {
		t.Fatalf("image_refs len = %d, want 1", len(imageRefs))
	}

	k8sRelationships, ok := got["k8s_relationships"].([]map[string]any)
	if !ok {
		t.Fatalf("k8s_relationships type = %T, want []map[string]any", got["k8s_relationships"])
	}
	if len(k8sRelationships) != 2 {
		t.Fatalf("k8s_relationships len = %d, want 2", len(k8sRelationships))
	}

	controllerDrivenPaths, ok := got["controller_driven_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_driven_paths type = %T, want []map[string]any", got["controller_driven_paths"])
	}
	if len(controllerDrivenPaths) != 1 {
		t.Fatalf("controller_driven_paths len = %d, want 1", len(controllerDrivenPaths))
	}

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 7 {
		t.Fatalf("delivery_paths len = %d, want 7", len(deliveryPaths))
	}

	drilldowns, ok := got["drilldowns"].(map[string]any)
	if !ok {
		t.Fatalf("drilldowns type = %T, want map[string]any", got["drilldowns"])
	}
	if drilldowns["service_context_path"] == "" {
		t.Fatal("drilldowns.service_context_path is empty, want service context route")
	}
}
