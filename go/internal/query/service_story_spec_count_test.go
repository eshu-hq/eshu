// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetServiceStorySpecCountsAgreeAcrossAPISurfaceAndSupportOverview(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j:   serviceStorySpecCountGraphReader{t: t},
		Content: fakePortContentStore{},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/sample-service-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "sample-service-api")
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
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	apiSurface := mapValue(data, "api_surface")
	supportOverview := mapValue(data, "support_overview")
	if got, want := IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got, want := IntVal(supportOverview, "spec_count"), 2; got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
}

type serviceStorySpecCountGraphReader struct {
	t *testing.T
}

func (g serviceStorySpecCountGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
		return []map[string]any{{"repo_id": "repo-sample-service-api", "repo_name": "sample-service-api"}}, nil
	case strings.Contains(cypher, "w.name = $service_name"):
		if got, want := params["service_name"], "sample-service-api"; got != want {
			g.t.Fatalf("params[service_name] = %#v, want %q", got, want)
		}
		return []map[string]any{{
			"id":        "workload:sample-service-api",
			"name":      "sample-service-api",
			"kind":      "service",
			"repo_id":   "repo-sample-service-api",
			"repo_name": "sample-service-api",
		}}, nil
	case strings.Contains(cypher, "RETURN count(endpoint) AS endpoint_count"):
		return []map[string]any{{"endpoint_count": 2}}, nil
	case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
		if got, want := params["limit"], repositoryAPISurfaceEndpointLimit; got != want {
			g.t.Fatalf("params[limit] = %#v, want %d", got, want)
		}
		return []map[string]any{
			{
				"endpoint_id":     "endpoint:public",
				"path":            "/v1/orders",
				"methods":         []string{"GET"},
				"operation_ids":   []string{"listOrders"},
				"source_kinds":    []string{"openapi"},
				"source_paths":    []string{"openapi/public.yaml"},
				"spec_versions":   []string{"3.0.3"},
				"api_versions":    []string{"v1"},
				"evidence_source": "graph",
				"workload_id":     "workload:sample-service-api",
				"workload_name":   "sample-service-api",
			},
			{
				"endpoint_id":     "endpoint:admin",
				"path":            "/admin/health",
				"methods":         []string{"GET"},
				"operation_ids":   []string{"getHealth"},
				"source_kinds":    []string{"openapi"},
				"source_paths":    []string{"openapi/admin.yaml"},
				"spec_versions":   []string{"3.1.0"},
				"api_versions":    []string{"admin"},
				"evidence_source": "graph",
				"workload_id":     "workload:sample-service-api",
				"workload_name":   "sample-service-api",
			},
		}, nil
	default:
		return nil, nil
	}
}

func (g serviceStorySpecCountGraphReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	if strings.Contains(cypher, "w.id = $workload_id") {
		return map[string]any{
			"id":        "workload:sample-service-api",
			"name":      "sample-service-api",
			"kind":      "service",
			"repo_id":   "repo-sample-service-api",
			"repo_name": "sample-service-api",
		}, nil
	}
	return nil, nil
}
