// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestFetchWorkloadContextUsesScalarQueriesForNornicDBOptionalProjectionSafety(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "collect(DISTINCT {") {
					t.Fatalf("cypher = %q, want scalar queries without optional map projection", cypher)
				}
				if !strings.Contains(cypher, "RETURN w.id as id, w.name as name, w.kind as kind") {
					t.Fatalf("unexpected RunSingle cypher: %q", cypher)
				}
				if got, want := params["service_name"], "svc-orders"; got != want {
					t.Fatalf("params[service_name] = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":   "workload:svc-orders",
					"name": "svc-orders",
					"kind": "service",
				}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "collect(DISTINCT {") {
					t.Fatalf("cypher = %q, want scalar queries without optional map projection", cypher)
				}
				if strings.Contains(cypher, "MATCH (i)-[runsOn:RUNS_ON]->") {
					t.Fatalf("cypher = %q, want exact instance and RUNS_ON traversal in one MATCH", cypher)
				}
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					if got, want := params["workload_id"], "workload:svc-orders"; got != want {
						t.Fatalf("params[workload_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"repo_id":   "repository:datax",
						"repo_name": "svc-orders",
					}}, nil
				case strings.Contains(cypher, "<-[rel:PROVISIONS_DEPENDENCY_FOR]-"):
					return nil, nil
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					for _, predicate := range []string{
						"repo.id = $repo_id", "w.id = $workload_id", "i.id IN $instance_ids",
					} {
						if !strings.Contains(cypher, predicate) {
							t.Fatalf("cypher = %q, want exact predicate %q", cypher, predicate)
						}
					}
					wantIDs := []string{
						"workload-instance:svc-orders:example-prod",
						"workload-instance:svc-orders:platform-qa",
					}
					if got := StringSliceVal(params, "instance_ids"); !reflect.DeepEqual(got, wantIDs) {
						t.Fatalf("params[instance_ids] = %#v, want %#v", got, wantIDs)
					}
					if got, want := StringVal(params, "repo_id"), "repository:datax"; got != want {
						t.Fatalf("params[repo_id] = %q, want %q", got, want)
					}
					return []map[string]any{
						{
							"instance_id":         "workload-instance:svc-orders:example-prod",
							"platform_id":         "platform:kubernetes:example-prod",
							"platform_name":       "example-prod",
							"platform_kind":       "kubernetes",
							"platform_confidence": 0.95,
							"platform_reason":     "resolved_deployment_evidence",
						},
						{
							"instance_id":         "workload-instance:svc-orders:example-prod",
							"platform_id":         "platform:ecs:ecs-prod",
							"platform_name":       "ecs-prod",
							"platform_kind":       "ecs",
							"platform_confidence": 0.91,
							"platform_reason":     "terraform_service_evidence",
						},
						{
							"instance_id":         "workload-instance:svc-orders:platform-qa",
							"platform_id":         "platform:kubernetes:platform-qa",
							"platform_name":       "platform-qa",
							"platform_kind":       "kubernetes",
							"platform_confidence": 0.95,
							"platform_reason":     "resolved_deployment_evidence",
						},
					}, nil
				case strings.Contains(cypher, "WHERE i.workload_id = $workload_id"):
					return []map[string]any{
						{
							"instance_id":                "workload-instance:svc-orders:example-prod",
							"environment":                "example-prod",
							"materialization_confidence": 0.91,
							"materialization_provenance": []any{"helm_values_reference"},
						},
						{
							"instance_id":                "workload-instance:svc-orders:platform-qa",
							"environment":                "platform-qa",
							"materialization_confidence": 0.91,
							"materialization_provenance": []any{"kustomize_resource_reference"},
						},
					}, nil
				case strings.Contains(cypher, "DEPENDS_ON|USES_MODULE|DEPLOYS_FROM"):
					return nil, nil
				case strings.Contains(cypher, "K8sResource OR"):
					return nil, nil
				case strings.Contains(cypher, "fn.name IN"):
					return nil, nil
				default:
					t.Fatalf("unexpected Run cypher: %q", cypher)
				}
				return nil, nil
			},
		},
	}

	ctx, err := handler.fetchWorkloadContext(
		context.Background(),
		"w.name = $service_name OR w.id = $service_name",
		map[string]any{"service_name": "svc-orders"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v", err)
	}

	if got, want := ctx["repo_name"], "svc-orders"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
	instances, ok := ctx["instances"].([]map[string]any)
	if !ok {
		t.Fatalf("instances type = %T, want []map[string]any", ctx["instances"])
	}
	if got, want := len(instances), 2; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	if got, want := instances[0]["environment"], "example-prod"; got != want {
		t.Fatalf("instances[0].environment = %#v, want %#v", got, want)
	}
	// Platforms attach in stable identity order (instance_id, platform_name,
	// platform_id) regardless of backend row order (#5644), so the
	// alphabetically-first platform name ("ecs-prod" < "example-prod") is
	// both instances[0].platforms[0] and the top-level convenience field.
	if got, want := instances[0]["platform_name"], "ecs-prod"; got != want {
		t.Fatalf("instances[0].platform_name = %#v, want %#v", got, want)
	}
	bgProdPlatforms := mapSliceValue(instances[0], "platforms")
	if got, want := len(bgProdPlatforms), 2; got != want {
		t.Fatalf("len(instances[0].platforms) = %d, want %d", got, want)
	}
	if got, want := bgProdPlatforms[1]["platform_name"], "example-prod"; got != want {
		t.Fatalf("instances[0].platforms[1].platform_name = %#v, want %#v", got, want)
	}
	if got, want := bgProdPlatforms[1]["platform_id"], "platform:kubernetes:example-prod"; got != want {
		t.Fatalf("instances[0].platforms[1].platform_id = %#v, want %#v", got, want)
	}
	if got, want := StringVal(bgProdPlatforms[1], "topology_basis"), "direct_runtime"; got != want {
		t.Fatalf("direct topology_basis = %#v, want %#v", got, want)
	}
	directRelationships := mapSliceValue(bgProdPlatforms[1], "topology_edges")
	if got, want := len(directRelationships), 1; got != want {
		t.Fatalf("len(instances[0].platforms[1].relationships) = %d, want %d", got, want)
	}
	if got, want := StringVal(directRelationships[0], "relationship_type"), "RUNS_ON"; got != want {
		t.Fatalf("direct relationship_type = %#v, want %#v", got, want)
	}
	if got, want := StringVal(directRelationships[0], "source_id"), "workload-instance:svc-orders:example-prod"; got != want {
		t.Fatalf("direct source_id = %#v, want %#v", got, want)
	}
	if got, want := StringVal(directRelationships[0], "target_id"), "platform:kubernetes:example-prod"; got != want {
		t.Fatalf("direct target_id = %#v, want %#v", got, want)
	}
	overview := buildServiceDeploymentOverview(ctx)
	if got, want := overview["platform_count"], 3; got != want {
		t.Fatalf("deployment_overview.platform_count = %#v, want %#v", got, want)
	}
	story := buildWorkloadStory(ctx)
	if !strings.Contains(story, "example-prod on ecs-prod (ecs), example-prod (kubernetes)") {
		t.Fatalf("story = %q, want both platform targets for example-prod in stable identity order", story)
	}
}

func TestFetchWorkloadContextPrefersInstanceRunsOnTruthOverProvisionedPlatformShortcut(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "RETURN w.id as id, w.name as name, w.kind as kind") {
					t.Fatalf("unexpected RunSingle cypher: %q", cypher)
				}
				return map[string]any{
					"id":      "workload:sample-service",
					"name":    "sample-service",
					"kind":    "service",
					"repo_id": "repository:r_fdb82379",
				}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{
						"repo_id": "repository:r_fdb82379", "repo_name": "sample-service",
					}}, nil
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					if strings.Contains(cypher, "MATCH (i)-[runsOn:RUNS_ON]->") {
						t.Fatalf("cypher = %q, want exact instance and RUNS_ON traversal in one MATCH", cypher)
					}
					for _, predicate := range []string{
						"repo.id = $repo_id", "w.id = $workload_id", "i.id IN $instance_ids",
					} {
						if !strings.Contains(cypher, predicate) {
							t.Fatalf("cypher = %q, want exact predicate %q", cypher, predicate)
						}
					}
					wantIDs := []string{
						"workload-instance:sample-service:example-prod",
						"workload-instance:sample-service:platform-qa",
					}
					if got := StringSliceVal(params, "instance_ids"); !reflect.DeepEqual(got, wantIDs) {
						t.Fatalf("params[instance_ids] = %#v, want %#v", got, wantIDs)
					}
					if got, want := StringVal(params, "repo_id"), "repository:r_fdb82379"; got != want {
						t.Fatalf("params[repo_id] = %q, want %q", got, want)
					}
					return []map[string]any{
						{
							"instance_id":         "workload-instance:sample-service:example-prod",
							"platform_name":       "example-prod",
							"platform_kind":       "kubernetes",
							"platform_confidence": nil,
							"platform_reason":     nil,
							"platform_edge": map[string]any{
								"confidence": 0.99,
								"reason":     "Workload instance runs on inferred platform",
							},
						},
						{
							"instance_id":         "workload-instance:sample-service:example-prod",
							"platform_name":       "shared-runtime-cluster",
							"platform_kind":       "ecs",
							"platform_confidence": 0.99,
							"platform_reason":     "Workload instance runs on inferred platform",
						},
						{
							"instance_id":         "workload-instance:sample-service:platform-qa",
							"platform_name":       "platform-qa",
							"platform_kind":       "kubernetes",
							"platform_confidence": 0.99,
							"platform_reason":     "Workload instance runs on inferred platform",
						},
					}, nil
				case strings.Contains(cypher, "WHERE i.workload_id = $workload_id"):
					if got, want := params["workload_id"], "workload:sample-service"; got != want {
						t.Fatalf("params[workload_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"instance_id":                "workload-instance:sample-service:example-prod",
							"environment":                "example-prod",
							"materialization_confidence": 0.96,
							"materialization_provenance": []any{"terraform_ecs_service"},
						},
						{
							"instance_id":                "workload-instance:sample-service:platform-qa",
							"environment":                "platform-qa",
							"materialization_confidence": 0.96,
							"materialization_provenance": []any{"terraform_ecs_service"},
						},
					}, nil
				case strings.Contains(cypher, "<-[rel:PROVISIONS_DEPENDENCY_FOR]-"):
					if got, want := params["repo_id"], "repository:r_fdb82379"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"platform_id":         "platform:ecs:aws:cluster/shared-runtime:none:none",
							"platform_name":       "shared-runtime-cluster",
							"platform_kind":       "ecs",
							"platform_confidence": 0.96,
							"platform_reason":     "Runtime services list declares repository dependency",
						},
					}, nil
				case strings.Contains(cypher, "DEPENDS_ON|USES_MODULE|DEPLOYS_FROM"):
					return nil, nil
				case strings.Contains(cypher, "K8sResource OR"):
					return nil, nil
				default:
					return nil, nil
				}
			},
		},
	}

	ctx, err := handler.fetchServiceWorkloadContext(
		context.Background(),
		"sample-service",
		"service_context",
	)
	if err != nil {
		t.Fatalf("fetchServiceWorkloadContext() error = %v, want nil", err)
	}

	instances, ok := ctx["instances"].([]map[string]any)
	if !ok {
		t.Fatalf("instances type = %T, want []map[string]any", ctx["instances"])
	}
	if got, want := len(instances), 2; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	bgProdPlatforms := mapSliceValue(instances[0], "platforms")
	if got, want := len(bgProdPlatforms), 2; got != want {
		t.Fatalf("len(example-prod platforms) = %d, want %d", got, want)
	}
	if got, want := instances[0]["platform_name"], "example-prod"; got != want {
		t.Fatalf("example-prod platform_name = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_confidence"], 0.99; got != want {
		t.Fatalf("example-prod platform_confidence = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_reason"], "Workload instance runs on inferred platform"; got != want {
		t.Fatalf("example-prod platform_reason = %#v, want %#v", got, want)
	}
	opsQAPlatforms := mapSliceValue(instances[1], "platforms")
	if got, want := len(opsQAPlatforms), 1; got != want {
		t.Fatalf("len(platform-qa platforms) = %d, want %d", got, want)
	}
	if got, want := instances[1]["platform_name"], "platform-qa"; got != want {
		t.Fatalf("platform-qa platform_name = %#v, want %#v", got, want)
	}
	if got, want := instances[1]["platform_kind"], "kubernetes"; got != want {
		t.Fatalf("platform-qa platform_kind = %#v, want %#v", got, want)
	}
}

func TestFetchDeploymentTraceKeepsProvisionedPlatformSeparateWhenInstanceRunsOnMissing(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
					return map[string]any{"repo_name": "legacy-service"}, nil
				}
				return map[string]any{
					"id":      "workload:legacy-service",
					"name":    "legacy-service",
					"kind":    "service",
					"repo_id": "repository:legacy",
				}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repository:legacy", "repo_name": "legacy-service"}}, nil
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					return nil, nil
				case strings.Contains(cypher, "WHERE i.workload_id = $workload_id"):
					return []map[string]any{{
						"instance_id":                "workload-instance:legacy-service:prod",
						"environment":                "prod",
						"materialization_confidence": 0.96,
					}}, nil
				case strings.Contains(cypher, "platformEdge:PROVISIONS_PLATFORM"):
					if !strings.Contains(cypher, "repo.id as platform_source_id") {
						t.Fatalf("provisioned-platform query lacks exact source endpoint:\n%s", cypher)
					}
					if !strings.Contains(cypher, "target.id as platform_dependency_target_id") {
						t.Fatalf("provisioned-platform query lacks dependency target endpoint:\n%s", cypher)
					}
					if !strings.Contains(cypher, "repo.name as platform_source_name") {
						t.Fatalf("provisioned-platform query lacks source display name:\n%s", cypher)
					}
					return []map[string]any{{
						"platform_source_id":            "repository:runtime-config",
						"platform_source_name":          "runtime-config",
						"platform_dependency_target_id": "repository:legacy",
						"platform_id":                   "platform:ecs:shared-runtime-cluster",
						"platform_name":                 "shared-runtime-cluster",
						"platform_kind":                 "ecs",
						"dependency_confidence":         0.96,
						"dependency_reason":             "Runtime services list declares repository dependency",
						"platform_edge_confidence":      0.93,
						"platform_edge_reason":          "Terraform declares runtime platform",
					}}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	ctx, err := handler.fetchServiceWorkloadContext(context.Background(), "legacy-service", "deployment_trace")
	if err != nil {
		t.Fatalf("fetchServiceWorkloadContext() error = %v, want nil", err)
	}
	instances := ctx["instances"].([]map[string]any)
	platforms := mapSliceValue(instances[0], "platforms")
	if len(platforms) != 0 {
		t.Fatalf("instance platforms = %#v, want direct RUNS_ON only", platforms)
	}
	provisioned := mapSliceValue(ctx, "provisioned_platforms")
	if got, want := len(provisioned), 1; got != want {
		t.Fatalf("provisioned_platforms = %#v, want %d", provisioned, want)
	}
	if got, want := provisioned[0]["platform_id"], "platform:ecs:shared-runtime-cluster"; got != want {
		t.Fatalf("provisioned platform_id = %#v, want %#v", got, want)
	}
	fallbackRelationships := mapSliceValue(provisioned[0], "topology_edges")
	if got, want := len(fallbackRelationships), 2; got != want {
		t.Fatalf("len(fallback relationships) = %d, want %d", got, want)
	}
	if got, want := StringVal(fallbackRelationships[0], "relationship_type"), "PROVISIONS_DEPENDENCY_FOR"; got != want {
		t.Fatalf("fallback dependency relationship_type = %#v, want %#v", got, want)
	}
	if got, want := StringVal(fallbackRelationships[0], "target_id"), "repository:legacy"; got != want {
		t.Fatalf("fallback dependency target_id = %#v, want %#v", got, want)
	}
	if got, want := StringVal(fallbackRelationships[1], "relationship_type"), "PROVISIONS_PLATFORM"; got != want {
		t.Fatalf("fallback platform relationship_type = %#v, want %#v", got, want)
	}
	if got, want := StringVal(fallbackRelationships[1], "source_id"), "repository:runtime-config"; got != want {
		t.Fatalf("fallback platform source_id = %#v, want %#v", got, want)
	}
	if got, want := StringVal(fallbackRelationships[1], "target_id"), "platform:ecs:shared-runtime-cluster"; got != want {
		t.Fatalf("fallback platform target_id = %#v, want %#v", got, want)
	}
}
