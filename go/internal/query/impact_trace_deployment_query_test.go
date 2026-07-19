// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestFetchDeploymentSourcesFallsBackToRepositoryDeployEdgesWhenNoCanonicalSourcesExist(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
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
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
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

func TestFetchDeploymentSourcesPreservesCanonicalAndRepositoryRelationshipOverlap(t *testing.T) {
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
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
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
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2 exact relationship families", len(got))
	}
	if got[0]["reason"] != "canonical_instance_deployment_source" {
		t.Fatalf("fetchDeploymentSources()[0].reason = %#v, want %#v", got[0]["reason"], "canonical_instance_deployment_source")
	}
}

func TestLoadUncorrelatedCloudResourceCandidatesUsesBoundedServiceSelector(t *testing.T) {
	t.Parallel()

	var seenCypher string
	var seenParams map[string]any
	got, err := loadUncorrelatedCloudResourceCandidates(t.Context(), fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			seenCypher = cypher
			seenParams = params
			return []map[string]any{
				{
					"id": "cloud:ssm:sample-service-client-port", "name": "/configd/sample-service/client/port",
					"resource_type": "ssm_parameter", "provider": "aws",
					"arn": "arn:aws:ssm:us-east-1:123456789012:parameter/configd/sample-service/client/port",
				},
			}, nil
		},
	}, "sample-service", 3)
	if err != nil {
		t.Fatalf("loadUncorrelatedCloudResourceCandidates() error = %v, want nil", err)
	}
	for _, want := range []string{"MATCH (n:CloudResource)", "LIMIT $limit", "coalesce(n.arn, '') CONTAINS $query"} {
		if !strings.Contains(seenCypher, want) {
			t.Fatalf("candidate cypher missing %q: %s", want, seenCypher)
		}
	}
	if strings.Contains(seenCypher, "MATCH (n)\n") || strings.Contains(seenCypher, "WHERE (n:CloudResource)") {
		t.Fatalf("candidate cypher must anchor the CloudResource label in MATCH, not scan all nodes: %s", seenCypher)
	}
	if strings.Contains(seenCypher, "toLower(") || strings.Contains(seenCypher, "$service_token") || strings.Contains(seenCypher, "$service_name") {
		t.Fatalf("candidate cypher must use infra-search-compatible parameterized CONTAINS shape: %s", seenCypher)
	}
	if got, want := seenParams["query"], "sample-service"; got != want {
		t.Fatalf("query = %#v, want %#v", got, want)
	}
	// The query over-fetches one beyond the bound (limit+1) so the caller can
	// surface explicit truncation instead of silently capping (issue #3378).
	if got, want := seenParams["limit"], 4; got != want {
		t.Fatalf("limit = %#v, want %#v (bound 3 + 1 over-fetch)", got, want)
	}
	if len(got) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(got))
	}
	if got, want := StringVal(got[0], "candidate_status"), "uncorrelated"; got != want {
		t.Fatalf("candidate_status = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "missing_relationship"), "workload_cloud_relationship"; got != want {
		t.Fatalf("missing_relationship = %q, want %q", got, want)
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

func TestTraceDeploymentChainKeepsConfigDerivedCloudResources(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:orders-api",
					"name":      "orders-api",
					"kind":      "service",
					"repo_id":   "repo-orders",
					"repo_name": "orders-api",
					"instances": []any{},
					"deployment_evidence": map[string]any{
						"artifacts": []map[string]any{
							{
								"relationship_type": "READS_CONFIG_FROM",
								"matched_value":     "/config/orders-api/*",
							},
						},
					},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)"):
					return nil, nil
				case strings.Contains(cypher, "MATCH (c:CloudResource)"):
					return []map[string]any{
						{
							"id":            "cloud-resource:ssm-config",
							"name":          "/config/orders-api/database-url",
							"resource_type": "aws_ssm_parameter",
							"provider":      "aws",
							"resource_id":   "arn:aws:ssm:example:parameter/config/orders-api/database-url",
						},
					}, nil
				case strings.Contains(cypher, "MATCH (n:CloudResource)"):
					return nil, nil
				default:
					return nil, nil
				}
			},
		},
		Content: NewContentReader(db),
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api"}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	resources := mapSliceValue(body, "cloud_resources")
	if got, want := len(resources), 1; got != want {
		t.Fatalf("cloud_resources len = %d, want %d; body = %#v", got, want, body)
	}
	if got, want := StringVal(resources[0], "relationship_basis"), "deployment_config_read_evidence"; got != want {
		t.Fatalf("relationship_basis = %q, want %q", got, want)
	}
	if candidates := mapSliceValue(body, "uncorrelated_cloud_resources"); len(candidates) != 0 {
		t.Fatalf("uncorrelated_cloud_resources = %#v, want omitted", candidates)
	}
	overview := mapValue(body, "deployment_overview")
	if got, want := IntVal(overview, "cloud_resource_count"), 1; got != want {
		t.Fatalf("deployment_overview.cloud_resource_count = %d, want %d", got, want)
	}
	summary := mapValue(body, "deployment_fact_summary")
	if missing := StringSliceVal(summary, "missing_evidence"); len(missing) != 0 {
		t.Fatalf("deployment_fact_summary.missing_evidence = %#v, want empty", missing)
	}
}
