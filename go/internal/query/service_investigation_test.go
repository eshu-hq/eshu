// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInvestigateServiceRouteReturnsCoverageAndRecommendations(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":      "workload:service-edge-api",
					"name":    "service-edge-api",
					"kind":    "service",
					"repo_id": "repo-service-edge-api",
				},
				"MATCH (r:Repository {id: $repo_id})": {
					"repo_name": "service-edge-api",
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.name = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/service-edge-api?intent=runbook&question=explain", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "service-edge-api")
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
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if _, ok := data["coverage_summary"]; !ok {
		t.Fatalf("coverage_summary missing from investigation response: %#v", data)
	}
	if _, ok := data["recommended_next_calls"]; !ok {
		t.Fatalf("recommended_next_calls missing from investigation response: %#v", data)
	}
}
