package query

import (
	"context"
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
				if got, want := params["service_name"], "api-node-datax"; got != want {
					t.Fatalf("params[service_name] = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":   "workload:api-node-datax",
					"name": "api-node-datax",
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
				case strings.Contains(cypher, "MATCH (r:Repository)-[:DEFINES]->(w)"):
					return []map[string]any{{
						"repo_id":   "repository:datax",
						"repo_name": "api-node-datax",
					}}, nil
				case strings.Contains(cypher, "<-[rel:PROVISIONS_DEPENDENCY_FOR]-"):
					return nil, nil
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					if len(params) != 0 {
						t.Fatalf("RUNS_ON params = %#v, want literal exact-instance anchor", params)
					}
					switch {
					case strings.Contains(cypher, "'workload-instance:api-node-datax:bg-prod'"):
						if !strings.Contains(cypher, "(i:WorkloadInstance {id: 'workload-instance:api-node-datax:bg-prod'})-[runsOn:RUNS_ON]->(p:Platform)") {
							t.Fatalf("cypher = %q, want compound exact-instance RUNS_ON pattern", cypher)
						}
						return []map[string]any{
							{
								"instance_id":         "workload-instance:api-node-datax:bg-prod",
								"platform_name":       "bg-prod",
								"platform_kind":       "kubernetes",
								"platform_confidence": 0.95,
								"platform_reason":     "resolved_deployment_evidence",
							},
							{
								"instance_id":         "workload-instance:api-node-datax:bg-prod",
								"platform_name":       "ecs-prod",
								"platform_kind":       "ecs",
								"platform_confidence": 0.91,
								"platform_reason":     "terraform_service_evidence",
							},
						}, nil
					case strings.Contains(cypher, "'workload-instance:api-node-datax:ops-qa'"):
						if !strings.Contains(cypher, "(i:WorkloadInstance {id: 'workload-instance:api-node-datax:ops-qa'})-[runsOn:RUNS_ON]->(p:Platform)") {
							t.Fatalf("cypher = %q, want compound exact-instance RUNS_ON pattern", cypher)
						}
						return []map[string]any{
							{
								"instance_id":         "workload-instance:api-node-datax:ops-qa",
								"platform_name":       "ops-qa",
								"platform_kind":       "kubernetes",
								"platform_confidence": 0.95,
								"platform_reason":     "resolved_deployment_evidence",
							},
						}, nil
					default:
						t.Fatalf("cypher = %q, want known exact instance anchor", cypher)
					}
				case strings.Contains(cypher, "WHERE i.workload_id = $workload_id"):
					return []map[string]any{
						{
							"instance_id":                "workload-instance:api-node-datax:bg-prod",
							"environment":                "bg-prod",
							"materialization_confidence": 0.91,
							"materialization_provenance": []any{"helm_values_reference"},
						},
						{
							"instance_id":                "workload-instance:api-node-datax:ops-qa",
							"environment":                "ops-qa",
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
		map[string]any{"service_name": "api-node-datax"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v", err)
	}

	if got, want := ctx["repo_name"], "api-node-datax"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
	instances, ok := ctx["instances"].([]map[string]any)
	if !ok {
		t.Fatalf("instances type = %T, want []map[string]any", ctx["instances"])
	}
	if got, want := len(instances), 2; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	if got, want := instances[0]["environment"], "bg-prod"; got != want {
		t.Fatalf("instances[0].environment = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_name"], "bg-prod"; got != want {
		t.Fatalf("instances[0].platform_name = %#v, want %#v", got, want)
	}
	bgProdPlatforms := mapSliceValue(instances[0], "platforms")
	if got, want := len(bgProdPlatforms), 2; got != want {
		t.Fatalf("len(instances[0].platforms) = %d, want %d", got, want)
	}
	if got, want := bgProdPlatforms[1]["platform_name"], "ecs-prod"; got != want {
		t.Fatalf("instances[0].platforms[1].platform_name = %#v, want %#v", got, want)
	}
	overview := buildServiceDeploymentOverview(ctx)
	if got, want := overview["platform_count"], 3; got != want {
		t.Fatalf("deployment_overview.platform_count = %#v, want %#v", got, want)
	}
	story := buildWorkloadStory(ctx)
	if !strings.Contains(story, "bg-prod on bg-prod (kubernetes), ecs-prod (ecs)") {
		t.Fatalf("story = %q, want both platform targets for bg-prod", story)
	}
}

func TestFetchWorkloadContextPrefersInstanceRunsOnTruthOverProvisionedPlatformShortcut(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
					return map[string]any{"repo_name": "sample-service"}, nil
				}
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
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					if len(params) != 0 {
						t.Fatalf("RUNS_ON params = %#v, want literal exact-instance anchor", params)
					}
					if strings.Contains(cypher, "MATCH (i)-[runsOn:RUNS_ON]->") {
						t.Fatalf("cypher = %q, want exact instance and RUNS_ON traversal in one MATCH", cypher)
					}
					if strings.Contains(cypher, "'workload-instance:sample-service:ops-qa'") {
						if !strings.Contains(cypher, "(i:WorkloadInstance {id: 'workload-instance:sample-service:ops-qa'})-[runsOn:RUNS_ON]->(p:Platform)") {
							t.Fatalf("cypher = %q, want compound exact-instance RUNS_ON pattern", cypher)
						}
						return []map[string]any{
							{
								"instance_id":         "workload-instance:sample-service:ops-qa",
								"platform_name":       "ops-qa",
								"platform_kind":       "kubernetes",
								"platform_confidence": 0.99,
								"platform_reason":     "Workload instance runs on inferred platform",
							},
						}, nil
					}
					if !strings.Contains(cypher, "'workload-instance:sample-service:bg-prod'") {
						t.Fatalf("cypher = %q, want exact instance id", cypher)
					}
					if !strings.Contains(cypher, "(i:WorkloadInstance {id: 'workload-instance:sample-service:bg-prod'})-[runsOn:RUNS_ON]->(p:Platform)") {
						t.Fatalf("cypher = %q, want compound exact-instance RUNS_ON pattern", cypher)
					}
					return []map[string]any{
						{
							"instance_id":         "workload-instance:sample-service:bg-prod",
							"platform_name":       "bg-prod",
							"platform_kind":       "kubernetes",
							"platform_confidence": nil,
							"platform_reason":     nil,
							"platform_edge": map[string]any{
								"confidence": 0.99,
								"reason":     "Workload instance runs on inferred platform",
							},
						},
						{
							"instance_id":         "workload-instance:sample-service:bg-prod",
							"platform_name":       "shared-runtime-cluster",
							"platform_kind":       "ecs",
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
							"instance_id":                "workload-instance:sample-service:bg-prod",
							"environment":                "bg-prod",
							"materialization_confidence": 0.96,
							"materialization_provenance": []any{"terraform_ecs_service"},
						},
						{
							"instance_id":                "workload-instance:sample-service:ops-qa",
							"environment":                "ops-qa",
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
		t.Fatalf("len(bg-prod platforms) = %d, want %d", got, want)
	}
	if got, want := instances[0]["platform_name"], "bg-prod"; got != want {
		t.Fatalf("bg-prod platform_name = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_confidence"], 0.99; got != want {
		t.Fatalf("bg-prod platform_confidence = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_reason"], "Workload instance runs on inferred platform"; got != want {
		t.Fatalf("bg-prod platform_reason = %#v, want %#v", got, want)
	}
	opsQAPlatforms := mapSliceValue(instances[1], "platforms")
	if got, want := len(opsQAPlatforms), 1; got != want {
		t.Fatalf("len(ops-qa platforms) = %d, want %d", got, want)
	}
	if got, want := instances[1]["platform_name"], "ops-qa"; got != want {
		t.Fatalf("ops-qa platform_name = %#v, want %#v", got, want)
	}
	if got, want := instances[1]["platform_kind"], "kubernetes"; got != want {
		t.Fatalf("ops-qa platform_kind = %#v, want %#v", got, want)
	}
}

func TestCypherStringLiteralEscapesInstanceIDs(t *testing.T) {
	t.Parallel()

	got := cypherStringLiteral("workload-instance:sample-service:prod's\\blue")
	want := `'workload-instance:sample-service:prod\'s\\blue'`
	if got != want {
		t.Fatalf("cypherStringLiteral() = %q, want %q", got, want)
	}
}

func TestFetchWorkloadContextFallsBackToProvisionedPlatformWhenInstanceRunsOnMissing(t *testing.T) {
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
				case strings.Contains(cypher, "-[runsOn:RUNS_ON]->(p:Platform)"):
					return nil, nil
				case strings.Contains(cypher, "WHERE i.workload_id = $workload_id"):
					return []map[string]any{{
						"instance_id":                "workload-instance:legacy-service:prod",
						"environment":                "prod",
						"materialization_confidence": 0.96,
					}}, nil
				case strings.Contains(cypher, "<-[rel:PROVISIONS_DEPENDENCY_FOR]-"):
					return []map[string]any{{
						"platform_name":       "shared-runtime-cluster",
						"platform_kind":       "ecs",
						"platform_confidence": 0.96,
						"platform_reason":     "Runtime services list declares repository dependency",
					}}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	ctx, err := handler.fetchServiceWorkloadContext(context.Background(), "legacy-service", "service_context")
	if err != nil {
		t.Fatalf("fetchServiceWorkloadContext() error = %v, want nil", err)
	}
	instances := ctx["instances"].([]map[string]any)
	if got, want := instances[0]["platform_name"], "shared-runtime-cluster"; got != want {
		t.Fatalf("fallback platform_name = %#v, want %#v", got, want)
	}
	if got, want := instances[0]["platform_kind"], "ecs"; got != want {
		t.Fatalf("fallback platform_kind = %#v, want %#v", got, want)
	}
}
