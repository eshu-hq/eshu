package query

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFetchDeploymentSourcesFallsBackToRepositoryDeployEdgesWhenNoCanonicalSourcesExist(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason": {
				{
					"repo_id":    "repo-helm",
					"repo_name":  "deployment-helm",
					"confidence": 0.93,
					"reason":     "helm_values_reference",
				},
				{
					"repo_id":    "repo-kustomize",
					"repo_name":  "deployment-kustomize",
					"confidence": 0.91,
					"reason":     "kustomize_resource_reference",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2", len(got))
	}
	if got[0]["repo_name"] != "deployment-helm" {
		t.Fatalf("fetchDeploymentSources()[0].repo_name = %#v, want %#v", got[0]["repo_name"], "deployment-helm")
	}
	if got[0]["reason"] != "helm_values_reference" {
		t.Fatalf("fetchDeploymentSources()[0].reason = %#v, want %#v", got[0]["reason"], "helm_values_reference")
	}
	if got[1]["repo_name"] != "deployment-kustomize" {
		t.Fatalf("fetchDeploymentSources()[1].repo_name = %#v, want %#v", got[1]["repo_name"], "deployment-kustomize")
	}
	if got[1]["reason"] != "kustomize_resource_reference" {
		t.Fatalf("fetchDeploymentSources()[1].reason = %#v, want %#v", got[1]["reason"], "kustomize_resource_reference")
	}
}

func TestFetchDeploymentSourcesMergesCanonicalAndRepositorySources(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {
				{
					"repo_id":    "repo-runtime-deploy",
					"repo_name":  "runtime-deploy",
					"confidence": 0.97,
					"reason":     "canonical_instance_deployment_source",
				},
			},
			"coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason": {
				{
					"repo_id":    "repo-legacy-deploy",
					"repo_name":  "legacy-deploy",
					"confidence": 0.62,
					"reason":     "repository_deploys_from",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2", len(got))
	}
	if got[0]["repo_name"] != "runtime-deploy" {
		t.Fatalf("fetchDeploymentSources()[0].repo_name = %#v, want %#v", got[0]["repo_name"], "runtime-deploy")
	}
	if got[1]["repo_name"] != "legacy-deploy" {
		t.Fatalf("fetchDeploymentSources()[1].repo_name = %#v, want %#v", got[1]["repo_name"], "legacy-deploy")
	}
}

func TestFetchDeploymentSourcesDedupesCanonicalAndRepositoryOverlap(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {
				{
					"repo_id":    "repo-runtime-deploy",
					"repo_name":  "runtime-deploy",
					"confidence": 0.97,
					"reason":     "canonical_instance_deployment_source",
				},
			},
			"coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason": {
				{
					"repo_id":    "repo-runtime-deploy",
					"repo_name":  "runtime-deploy",
					"confidence": 0.62,
					"reason":     "repository_deploys_from",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 1", len(got))
	}
	if got[0]["reason"] != "canonical_instance_deployment_source" {
		t.Fatalf("fetchDeploymentSources()[0].reason = %#v, want %#v", got[0]["reason"], "canonical_instance_deployment_source")
	}
}

func TestFetchServiceTraceContextAcceptsQualifiedWorkloadID(t *testing.T) {
	t.Parallel()

	seenBroadServiceLookup := false
	ctx, err := fetchServiceTraceContext(
		t.Context(),
		fakeWorkloadGraphReader{
			runSingle: func(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, " OR ") {
					seenBroadServiceLookup = true
					return nil, errors.New("broad service lookup should not run")
				}
				if strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if strings.Contains(cypher, "w.id = $service_name") {
					return map[string]any{
						"id":        "workload:service-edge-api",
						"name":      "service-edge-api",
						"kind":      "service",
						"repo_id":   "repo-service-edge-api",
						"repo_name": "service-edge-api",
						"instances": []any{
							map[string]any{
								"instance_id":   "instance:service-edge-api:modern",
								"platform_name": "modern-cluster",
								"platform_kind": "kubernetes",
								"environment":   "modern",
							},
						},
					}, nil
				}
				return nil, nil
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		nil,
		nil,
		"workload:service-edge-api",
		traceEnrichmentOptions(traceDeploymentChainRequest{ServiceName: "workload:service-edge-api"}),
	)
	if err != nil {
		t.Fatalf("fetchServiceTraceContext() error = %v, want nil", err)
	}
	if seenBroadServiceLookup {
		t.Fatal("fetchServiceTraceContext used broad service OR lookup")
	}
	if got, want := safeStr(ctx, "id"), "workload:service-edge-api"; got != want {
		t.Fatalf("context.id = %#v, want %#v", got, want)
	}
	if got, want := safeStr(ctx, "name"), "service-edge-api"; got != want {
		t.Fatalf("context.name = %#v, want %#v", got, want)
	}
}

func TestFetchServiceTraceContextIncludesGraphDeploymentEvidenceWithoutContent(t *testing.T) {
	t.Parallel()

	ctx, err := fetchServiceTraceContext(
		t.Context(),
		fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:checkout-service",
					"name":      "checkout-service",
					"kind":      "service",
					"repo_id":   "repo-service",
					"repo_name": "checkout-service",
					"instances": []any{},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
				"EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository": {
					{
						"direction":         "incoming",
						"artifact_id":       "evidence-artifact:kustomize:1",
						"name":              "apps/checkout/kustomization.yaml",
						"domain":            "deployment",
						"path":              "apps/checkout/kustomization.yaml",
						"evidence_kind":     "KUSTOMIZE_RESOURCE_REFERENCE",
						"artifact_family":   "kustomize",
						"extractor":         "kustomize",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-kustomize",
						"generation_id":     "gen-deploy",
						"confidence":        0.9,
						"environment":       "prod",
						"matched_alias":     "checkout-service",
						"matched_value":     "checkout-service",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-deploy",
						"source_repo_name":  "deployment-configs",
						"target_repo_id":    "repo-service",
						"target_repo_name":  "checkout-service",
					},
				},
				"(r:Repository {id: $repo_id})-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->": {},
			},
		},
		nil,
		nil,
		"checkout-service",
		traceEnrichmentOptions(traceDeploymentChainRequest{ServiceName: "checkout-service"}),
	)
	if err != nil {
		t.Fatalf("fetchServiceTraceContext() error = %v, want nil", err)
	}

	evidence := mapValue(ctx, "deployment_evidence")
	if len(evidence) == 0 {
		t.Fatal("deployment_evidence = nil, want graph-backed deployment evidence")
	}
	if got, want := evidence["truth_basis"], "graph"; got != want {
		t.Fatalf("deployment_evidence.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := evidence["artifact_count"], 1; got != want {
		t.Fatalf("deployment_evidence.artifact_count = %#v, want %#v", got, want)
	}

	response := buildDeploymentTraceResponse("checkout-service", ctx)
	traceEvidence := mapValue(response, "deployment_evidence")
	if got, want := traceEvidence["artifact_count"], 1; got != want {
		t.Fatalf("trace deployment_evidence.artifact_count = %#v, want %#v", got, want)
	}
	deploymentOverview := mapValue(response, "deployment_overview")
	if !slices.Contains(stringSliceValue(deploymentOverview, "deployment_tool_families"), "kustomize") {
		t.Fatalf("deployment_overview.deployment_tool_families = %#v, want kustomize", deploymentOverview["deployment_tool_families"])
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
			},
			{
				"instance_id":   "inst-2",
				"platform_name": "payments-argocd",
				"platform_kind": "argocd_application",
				"environment":   "stage",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-deploy",
				"repo_name":  "payments-deploy",
				"confidence": 0.98,
				"reason":     "Deployment manifests for workload instance live in deployment repository",
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

func TestBuildDeploymentFactsIncludesEveryRuntimePlatformTarget(t *testing.T) {
	t.Parallel()

	facts := buildDeploymentFacts([]map[string]any{
		{
			"instance_id":                "instance:sample-service:production",
			"environment":                "production",
			"materialization_confidence": 0.92,
			"platforms": []map[string]any{
				{
					"platform_name":       "production-eks",
					"platform_kind":       "kubernetes",
					"platform_confidence": 0.95,
				},
				{
					"platform_name":       "production-ecs",
					"platform_kind":       "ecs",
					"platform_confidence": 0.91,
				},
			},
		},
	}, nil)

	targets := map[string]bool{}
	for _, fact := range facts {
		if StringVal(fact, "type") == "RUNS_ON_PLATFORM" {
			targets[StringVal(fact, "target")] = true
		}
	}
	for _, want := range []string{"production-ecs", "production-eks"} {
		if !targets[want] {
			t.Fatalf("RUNS_ON_PLATFORM targets = %#v, want %q", targets, want)
		}
	}
}

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
				"repository":     "api-node-saved-search",
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

func TestBuildDeploymentTraceResponseDeduplicatesRepositoryDeliveryPaths(t *testing.T) {
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
				"repo_id":    "repo-helm",
				"repo_name":  "deployment-helm",
				"confidence": 0.98,
			},
		},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{
					"type":   "deployment_source",
					"target": "deployment-helm",
				},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if gotCount, want := len(deliveryPaths), 2; gotCount != want {
		t.Fatalf("len(delivery_paths) = %d, want %d", gotCount, want)
	}
	if got, want := StringVal(deliveryPaths[0], "target"), "deployment-helm"; got != want {
		t.Fatalf("delivery_paths[0].target = %q, want %q", got, want)
	}
	if got, want := StringVal(deliveryPaths[1], "type"), "repository_delivery_artifact"; got != want {
		t.Fatalf("delivery_paths[1].type = %q, want %q", got, want)
	}
}

func TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths(t *testing.T) {
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
				"platform_name": "modern",
				"platform_kind": "kubernetes",
				"environment":   "prod",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-kustomize",
				"repo_name":  "deployment-kustomize",
				"confidence": 0.98,
			},
		},
		"image_refs": []string{"ghcr.io/acme/payments-api:1.2.3"},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	for _, path := range deliveryPaths {
		if StringVal(path, "type") == "" {
			t.Fatalf("delivery path missing type: %#v", path)
		}
		if StringVal(path, "type") == "" &&
			StringVal(path, "target") == "" &&
			StringVal(path, "path") == "" &&
			StringVal(path, "kind") == "" &&
			StringVal(path, "artifact_type") == "" &&
			StringVal(path, "evidence_kind") == "" {
			t.Fatalf("structurally empty delivery path leaked: %#v", path)
		}
	}
}

func TestBuildDeploymentTraceResponseSeparatesControllerIdentityFromObservedTargets(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "service",
		"repo_id":   "repository:r_service_edge_api",
		"repo_name": "service-edge-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:service-edge-api:modern",
				"platform_name": "modern",
				"platform_kind": "kubernetes",
				"environment":   "modern",
			},
		},
	}

	got := buildDeploymentTraceResponse("service-edge-api", ctx)

	controllerOverview, ok := got["controller_overview"].(map[string]any)
	if !ok {
		t.Fatalf("controller_overview type = %T, want map[string]any", got["controller_overview"])
	}
	if gotControllers := StringSliceVal(controllerOverview, "controllers"); len(gotControllers) != 0 {
		t.Fatalf("controller_overview.controllers = %#v, want empty when no controller entities exist", gotControllers)
	}
	if gotTargets, wantTargets := StringSliceVal(controllerOverview, "observed_targets"), []string{"modern"}; !reflect.DeepEqual(gotTargets, wantTargets) {
		t.Fatalf("controller_overview.observed_targets = %#v, want %#v", gotTargets, wantTargets)
	}
	if gotKinds, wantKinds := StringSliceVal(controllerOverview, "controller_kinds"), []string{"kubernetes"}; !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("controller_overview.controller_kinds = %#v, want %#v", gotKinds, wantKinds)
	}

	controllerDrivenPaths, ok := got["controller_driven_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_driven_paths type = %T, want []map[string]any", got["controller_driven_paths"])
	}
	if gotCount, wantCount := len(controllerDrivenPaths), 1; gotCount != wantCount {
		t.Fatalf("len(controller_driven_paths) = %d, want %d", gotCount, wantCount)
	}
	if gotController := StringVal(controllerDrivenPaths[0], "controller"); gotController != "" {
		t.Fatalf("controller_driven_paths[0].controller = %q, want empty when only observed target is known", gotController)
	}
	if gotTarget, wantTarget := StringVal(controllerDrivenPaths[0], "observed_target"), "modern"; gotTarget != wantTarget {
		t.Fatalf("controller_driven_paths[0].observed_target = %q, want %q", gotTarget, wantTarget)
	}
}
