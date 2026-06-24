// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetServiceStoryReadbackAlignsSupportOverviewSpecCountWithAPISurface(t *testing.T) {
	t.Parallel()

	const (
		repoID      = "repo-service-edge-api"
		serviceID   = "workload:service-edge-api"
		serviceName = "service-edge-api"
	)
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.name = $service_name"):
					if got := params["service_name"]; got != serviceName {
						t.Fatalf("service_name param = %#v, want %q", got, serviceName)
					}
					return []map[string]any{{"id": serviceID, "name": serviceName, "kind": "service", "repo_id": repoID}}, nil
				case strings.Contains(cypher, "r.id IN $repo_ids"):
					return []map[string]any{{"repo_id": repoID, "repo_name": serviceName}}, nil
				case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
					return []map[string]any{{"endpoint_count": 2}}, nil
				case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
					return []map[string]any{
						{
							"endpoint_id":   "endpoint-list",
							"path":          "/v1/widgets",
							"methods":       []any{"GET"},
							"source_kinds":  []any{"openapi"},
							"source_paths":  []any{"openapi.yaml"},
							"workload_id":   serviceID,
							"workload_name": serviceName,
						},
						{
							"endpoint_id":   "endpoint-admin",
							"path":          "/admin/health",
							"methods":       []any{"GET"},
							"source_kinds":  []any{"openapi"},
							"source_paths":  []any{"admin.yaml"},
							"workload_id":   serviceID,
							"workload_name": serviceName,
						},
					}, nil
				case strings.Contains(cypher, "INSTANCE_OF"),
					strings.Contains(cypher, "WorkloadInstance"),
					strings.Contains(cypher, "DEPENDS_ON|USES_MODULE|DEPLOYS_FROM"),
					strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE"),
					strings.Contains(cypher, "K8sResource OR"),
					strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE"),
					strings.Contains(cypher, "CloudResource"),
					strings.Contains(cypher, "fn.name IN"):
					return nil, nil
				default:
					t.Fatalf("unexpected graph query:\n%s", cypher)
					return nil, nil
				}
			},
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.id = $workload_id"):
					return map[string]any{"id": serviceID, "name": serviceName, "kind": "service", "repo_id": repoID}, nil
				case strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})"):
					return map[string]any{"repo_id": repoID, "repo_name": serviceName}, nil
				default:
					t.Fatalf("unexpected graph single query:\n%s", cypher)
					return nil, nil
				}
			},
		},
		Content: fakePortContentStore{},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", serviceName)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", envelope.Data)
	}
	apiSurface := mapValue(data, "api_surface")
	supportOverview := mapValue(data, "support_overview")
	if got, want := IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got, want := IntVal(supportOverview, "spec_count"), IntVal(apiSurface, "spec_count"); got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
	if story := StringVal(data, "story"); !strings.Contains(story, "2 spec file(s)") {
		t.Fatalf("story = %q, want normalized spec count", story)
	}
}

func TestBuildServiceStoryResponseNormalizesAPISurfaceOnce(t *testing.T) {
	ctx := serviceStoryAPISurfaceJSONContext(75)

	allocs := testing.AllocsPerRun(100, func() {
		_ = buildServiceStoryResponse("service-edge-api", ctx)
	})

	// GitHub Actions runs this package under the race detector, which adds a few
	// bookkeeping allocations. Keep the guard tight enough to catch repeated
	// API-surface normalization while allowing the race-instrumented build.
	const maxAllocsPerResponse = 690
	if allocs > maxAllocsPerResponse {
		t.Fatalf("allocs per service story response = %.0f, want <= %d", allocs, maxAllocsPerResponse)
	}
}

func serviceStoryAPISurfaceJSONContext(endpointCount int) map[string]any {
	endpoints := make([]any, 0, endpointCount)
	for i := range endpointCount {
		endpoints = append(endpoints, map[string]any{
			"path":      fmt.Sprintf("/v1/resource-%03d", i),
			"methods":   []any{"GET", "POST"},
			"spec_path": "openapi.yaml",
		})
	}
	return map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "service",
		"repo_id":   "repo-service-edge-api",
		"repo_name": "service-edge-api",
		"api_surface": map[string]any{
			"endpoint_count": endpointCount,
			"method_count":   endpointCount * 2,
			"spec_paths":     []any{"openapi.yaml"},
			"endpoints":      endpoints,
		},
	}
}
