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

// TestGetServiceContextAddsPartialReasons is a round-11 review follow-up to
// #5764 (PR #5936, chatgpt-codex-connector finding 1): the OpenAPI
// WorkloadContext schema (openapi_components_workload_session.go) documents
// "partial_reasons" as an always-present field "so the envelope shape is
// stable across complete and partial reads", and getWorkloadContext
// (entity_workload_handlers.go) honors that by calling
// ctx["partial_reasons"] = contextPartialReasons(ctx) before WriteSuccess.
// getServiceContext (entity.go) writes the fetched workload-context map
// straight to WriteSuccess without that call, so an infrastructure-read
// degradation that lands in "limitations" (fetchWorkloadContextForOperation,
// entity_workload_context.go) was visible on GET
// /api/v0/workloads/{workload_id}/context but silently absent on GET
// /api/v0/services/{service_name}/context, even though both routes share the
// same WorkloadContext response schema. This test drives the real handler
// through its mounted route (not a helper) and fails before the fix because
// body["partial_reasons"] is entirely absent.
func TestGetServiceContextAddsPartialReasons(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:svc-partial-reasons",
					"name":      "svc-partial-reasons",
					"kind":      "service",
					"repo_id":   "repo-svc-partial-reasons",
					"repo_name": "svc-partial-reasons",
					"instances": []any{},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-svc-partial-reasons", "repo_name": "svc-partial-reasons"}}, nil
				case strings.Contains(cypher, infrastructureGraphReadCypherFragment):
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphReadDeadline)
				default:
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/svc-partial-reasons/context", nil)
	req.SetPathValue("service_name", "svc-partial-reasons")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// The degraded infrastructure read must still be visible under
	// "limitations" (the raw field this handler already wrote).
	limitations, ok := body["limitations"].([]any)
	if !ok || !jsonStringSliceContains(limitations, infrastructureReadDegradedReason) {
		t.Fatalf("body[limitations] = %#v, want to contain %q", body["limitations"], infrastructureReadDegradedReason)
	}

	// The OpenAPI-promised "partial_reasons" field must promote that same
	// reason, matching getWorkloadContext's sibling route.
	partialReasons, ok := body["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("body[partial_reasons] missing or wrong type: %#v", body["partial_reasons"])
	}
	if !jsonStringSliceContains(partialReasons, infrastructureReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want to contain %q", partialReasons, infrastructureReadDegradedReason)
	}
}
