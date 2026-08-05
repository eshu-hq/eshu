// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetServiceContextInfrastructureDegradeAttributesFailure covers a fourth
// #5764 call site: fetchWorkloadContextForOperation (entity_workload_context.go),
// reached from getServiceContext's GET /api/v0/services/{service_name}/context.
// Before this fix, queryRepoInfrastructure's degraded bool was routed only to
// the stage log -- the result map never carried a limitations reason -- so a
// degraded infrastructure read answered "infrastructure": [] indistinguishable
// from a repository that genuinely has no infrastructure, exactly the defect
// #5764 exists to remove. This response is the raw workload-context map (no
// intermediate response builder), so the reason must appear directly under the
// top-level "limitations" key, mirroring the sibling
// fetchServiceReadModelWorkloadContext's existing "limitations" field.
func TestGetServiceContextInfrastructureDegradeAttributesFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:svc-infra-degrade",
					"name":      "svc-infra-degrade",
					"kind":      "service",
					"repo_id":   "repo-svc-infra-degrade",
					"repo_name": "svc-infra-degrade",
					"instances": []any{},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-svc-infra-degrade", "repo_name": "svc-infra-degrade"}}, nil
				case strings.Contains(cypher, infrastructureGraphReadCypherFragment):
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphReadDeadline)
				default:
					return nil, nil
				}
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/svc-infra-degrade/context", nil)
	req.SetPathValue("service_name", "svc-infra-degrade")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := body["name"]; !ok {
		t.Fatalf("body missing name field, rest of response not intact: %s", w.Body.String())
	}
	infrastructure, ok := body["infrastructure"].([]any)
	if !ok || len(infrastructure) != 0 {
		t.Fatalf("body[infrastructure] = %#v, want empty list (degraded, not fabricated rows)", body["infrastructure"])
	}

	limitations, ok := body["limitations"].([]any)
	if !ok {
		t.Fatalf("body[limitations] missing or wrong type: %#v", body["limitations"])
	}
	if !jsonStringSliceContains(limitations, infrastructureReadDegradedReason) {
		t.Fatalf("limitations = %#v, want to contain %q", limitations, infrastructureReadDegradedReason)
	}

	logText := logs.String()
	if !strings.Contains(logText, `"stage":"repo_infrastructure"`) {
		t.Fatalf("logs missing repo_infrastructure stage; logs = %s", logText)
	}
	if !strings.Contains(logText, `"failure_class":"`+infrastructureReadDegradedReason+`"`) {
		t.Fatalf("logs missing failure_class=%s; logs = %s", infrastructureReadDegradedReason, logText)
	}
}

// TestGetServiceContextInfrastructureHealthyEmptyDoesNotDegrade is the negative
// companion: a healthy graph read that genuinely returns zero infrastructure
// rows (no error) must NOT add infrastructureReadDegradedReason to
// limitations or emit a failure_class log. This is what separates "no
// infrastructure" from "couldn't read infrastructure" (#5764).
func TestGetServiceContextInfrastructureHealthyEmptyDoesNotDegrade(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:svc-infra-healthy",
					"name":      "svc-infra-healthy",
					"kind":      "service",
					"repo_id":   "repo-svc-infra-healthy",
					"repo_name": "svc-infra-healthy",
					"instances": []any{},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
					return []map[string]any{{"repo_id": "repo-svc-infra-healthy", "repo_name": "svc-infra-healthy"}}, nil
				}
				return nil, nil
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/svc-infra-healthy/context", nil)
	req.SetPathValue("service_name", "svc-infra-healthy")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if limitations, ok := body["limitations"].([]any); ok && jsonStringSliceContains(limitations, infrastructureReadDegradedReason) {
		t.Fatalf("limitations = %#v, want no %q for a healthy empty read", limitations, infrastructureReadDegradedReason)
	}
	if strings.Contains(logs.String(), "failure_class") {
		t.Fatalf("logs unexpectedly carry failure_class for a healthy empty read; logs = %s", logs.String())
	}
}
