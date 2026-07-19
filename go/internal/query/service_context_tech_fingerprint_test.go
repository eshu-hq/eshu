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

// TestGetServiceContextIncludesTechFingerprint asserts that language_breakdown
// and source_tool_breakdown appear in the service context response when the
// graph returns data for the service's repository.
func TestGetServiceContextIncludesTechFingerprint(t *testing.T) {
	t.Parallel()

	reader := fakeWorkloadGraphReader{
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload)") || strings.Contains(cypher, "w.name = $service_name") {
				return map[string]any{
					"id":      "workload:fp-service",
					"name":    "fp-service",
					"kind":    "service",
					"repo_id": "repo-fp-svc",
				}, nil
			}
			if strings.Contains(cypher, "MATCH (r:Repository") {
				return map[string]any{"repo_name": "fp-service-repo"}, nil
			}
			return nil, nil
		},
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)": {
				{"repo_id": "repo-fp-svc", "repo_name": "fp-service-repo"},
			},
			"INSTANCE_OF":                         {},
			"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			"K8sResource OR":                      {},
			// language distribution — anchored on repo
			"f.language IS NOT NULL": {
				{"language": "python", "file_count": int64(30)},
				{"language": "yaml", "file_count": int64(8)},
			},
			// source_tool breakdown — anchored on repo
			"rel.source_tool IS NOT NULL": {
				{"source_tool": "ansible", "edge_count": int64(5)},
			},
		},
	}

	handler := &EntityHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/fp-service/context", nil)
	req.SetPathValue("service_name", "fp-service")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-fp",
		WorkspaceID:          "workspace-fp",
		AllowedRepositoryIDs: []string{"repo-fp-svc"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	langBreakdown, ok := resp["language_breakdown"].(map[string]any)
	if !ok || langBreakdown == nil {
		t.Fatalf("language_breakdown = %#v, want map[string]any", resp["language_breakdown"])
	}
	if got, want := langBreakdown["python"], float64(30); got != want {
		t.Errorf("language_breakdown[python] = %v, want %v", got, want)
	}

	toolBreakdown, ok := resp["source_tool_breakdown"].(map[string]any)
	if !ok || toolBreakdown == nil {
		t.Fatalf("source_tool_breakdown = %#v, want map[string]any", resp["source_tool_breakdown"])
	}
	if got, want := toolBreakdown["ansible"], float64(5); got != want {
		t.Errorf("source_tool_breakdown[ansible] = %v, want %v", got, want)
	}
}

// TestQueryServiceTechFingerprintOmitsBreakdownsWhenNoRepoID asserts that both
// breakdowns are nil when the workload context has no repo_id — no graph calls
// are made to avoid needless round-trips.
func TestQueryServiceTechFingerprintOmitsBreakdownsWhenNoRepoID(t *testing.T) {
	t.Parallel()

	var calls int
	reader := &captureGraphQuery{
		runFn: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			calls++
			return nil, nil
		},
	}

	workloadCtx := map[string]any{"name": "empty-repo-svc"} // no repo_id key
	langBreakdown, toolBreakdown := queryServiceTechFingerprint(context.Background(), reader, workloadCtx)

	if langBreakdown != nil {
		t.Errorf("language_breakdown = %v, want nil when repo_id missing", langBreakdown)
	}
	if toolBreakdown != nil {
		t.Errorf("source_tool_breakdown = %v, want nil when repo_id missing", toolBreakdown)
	}
	if calls != 0 {
		t.Errorf("graph calls = %d, want 0 when repo_id is empty", calls)
	}
}

// TestQueryServiceTechFingerprintSourceToolQueryIsAnchored asserts that the
// source_tool breakdown query issued inside queryServiceTechFingerprint is
// anchored on the repo_id and does not perform an all-node scan.
func TestQueryServiceTechFingerprintSourceToolQueryIsAnchored(t *testing.T) {
	t.Parallel()

	var capturedCyphers []string
	reader := &captureGraphQuery{
		runFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			capturedCyphers = append(capturedCyphers, cypher)
			return nil, nil
		},
	}

	workloadCtx := map[string]any{"repo_id": "repo-anchor-test"}
	queryServiceTechFingerprint(context.Background(), reader, workloadCtx)

	var sourceToolCypher string
	for _, c := range capturedCyphers {
		if strings.Contains(c, "source_tool IS NOT NULL") {
			sourceToolCypher = c
			break
		}
	}
	if sourceToolCypher == "" {
		t.Fatal("queryServiceTechFingerprint did not issue a source_tool query")
	}
	if !strings.Contains(sourceToolCypher, "Repository {id: $repo_id}") {
		t.Errorf("source_tool query is not repo-anchored:\n%s", sourceToolCypher)
	}
	if strings.Contains(sourceToolCypher, "MATCH ()-[") {
		t.Errorf("source_tool query uses all-node scan pattern:\n%s", sourceToolCypher)
	}
}
