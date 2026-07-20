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

// graphSummaryRecordingReader records every Cypher statement and routes the
// result by content match so each bounded count/degree query can be controlled
// independently and asserted for shape.
type graphSummaryRecordingReader struct {
	single     func(string, map[string]any) (map[string]any, error)
	multi      func(string, map[string]any) ([]map[string]any, error)
	recordOnly *[]string
}

func (g *graphSummaryRecordingReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if g.recordOnly != nil {
		*g.recordOnly = append(*g.recordOnly, cypher)
	}
	if g.multi != nil {
		return g.multi(cypher, params)
	}
	return nil, nil
}

func (g *graphSummaryRecordingReader) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if g.recordOnly != nil {
		*g.recordOnly = append(*g.recordOnly, cypher)
	}
	if g.single != nil {
		return g.single(cypher, params)
	}
	return nil, nil
}

func decodeGraphSummaryData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v; body=%s", err, body)
	}
	// Envelope responses nest the payload under "data"; non-envelope responses
	// return the payload directly.
	if data, ok := envelope["data"].(map[string]any); ok {
		return data
	}
	return envelope
}

func TestGraphSummaryPacketRepoScopedShapeIsBoundedAndDeterministic(t *testing.T) {
	t.Parallel()

	var captured []string
	reader := &graphSummaryRecordingReader{
		recordOnly: &captured,
		single: func(cypher string, params map[string]any) (map[string]any, error) {
			if got, want := params["repo_id"], "repo-1"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v in %s", got, want, cypher)
			}
			switch {
			case strings.Contains(cypher, "[r:CALLS]"):
				return map[string]any{"count": int64(40)}, nil
			case strings.Contains(cypher, "[r:IMPORTS]"):
				return map[string]any{"count": int64(12)}, nil
			case strings.Contains(cypher, "[r:INHERITS]"):
				return map[string]any{"count": int64(3)}, nil
			case strings.Contains(cypher, "[r:OVERRIDES]"):
				return map[string]any{"count": int64(0)}, nil
			case strings.Contains(cypher, "[r:REFERENCES]"):
				return map[string]any{"count": int64(5)}, nil
			case strings.Contains(cypher, "[:REPO_CONTAINS]->(f:File)") && strings.Contains(cypher, "count(DISTINCT f)"):
				return map[string]any{"count": int64(120)}, nil
			case strings.Contains(cypher, "(w:Workload)") && strings.Contains(cypher, "count(DISTINCT w)"):
				return map[string]any{"count": int64(4)}, nil
			case strings.Contains(cypher, "(p:Platform)") && strings.Contains(cypher, "count(DISTINCT p)"):
				return map[string]any{"count": int64(2)}, nil
			case strings.Contains(cypher, "DEPENDS_ON") && strings.Contains(cypher, "count(DISTINCT dep)"):
				return map[string]any{"count": int64(7)}, nil
			}
			return nil, nil
		},
		multi: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "total_degree"):
				return []map[string]any{
					{"function_id": "fn-a", "function_name": "Alpha", "file_path": "a.go", "incoming_calls": int64(10), "outgoing_calls": int64(5), "total_degree": int64(15)},
					{"function_id": "fn-b", "function_name": "Beta", "file_path": "b.go", "incoming_calls": int64(4), "outgoing_calls": int64(4), "total_degree": int64(8)},
				}, nil
			case strings.Contains(cypher, "f.language"):
				return []map[string]any{
					{"language": "go", "file_count": int64(100)},
					{"language": "python", "file_count": int64(20)},
				}, nil
			}
			return nil, nil
		},
	}

	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := strings.NewReader(`{"repo_id":"repo-1","limit":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}

	// No statement may chain two code relationship types in one query, mirroring
	// the per-label/per-type portability rule.
	for _, cypher := range captured {
		if strings.Contains(cypher, "[r:CALLS]") && strings.Contains(cypher, "[r:IMPORTS]") {
			t.Fatalf("relationship type counts chained in one statement:\n%s", cypher)
		}
	}

	data := decodeGraphSummaryData(t, w.Body.Bytes())

	hot, ok := data["hot_entities"].([]any)
	if !ok {
		t.Fatalf("hot_entities type = %T, want []any", data["hot_entities"])
	}
	if len(hot) != 2 {
		t.Fatalf("hot_entities len = %d, want 2", len(hot))
	}
	first := hot[0].(map[string]any)
	if got := first["total_degree"]; got != float64(15) {
		t.Fatalf("hot_entities[0].total_degree = %#v, want 15 (descending order)", got)
	}

	rel, ok := data["key_relationships"].(map[string]any)
	if !ok {
		t.Fatalf("key_relationships type = %T, want map", data["key_relationships"])
	}
	for typeName, want := range map[string]float64{"CALLS": 40, "IMPORTS": 12, "INHERITS": 3, "OVERRIDES": 0, "REFERENCES": 5} {
		if got := rel[typeName]; got != want {
			t.Fatalf("key_relationships[%s] = %#v, want %#v", typeName, got, want)
		}
	}

	eco, ok := data["ecosystem_map"].(map[string]any)
	if !ok {
		t.Fatalf("ecosystem_map type = %T, want map", data["ecosystem_map"])
	}
	if got := eco["file_count"]; got != float64(120) {
		t.Fatalf("ecosystem_map.file_count = %#v, want 120", got)
	}
	if got := eco["workload_count"]; got != float64(4) {
		t.Fatalf("ecosystem_map.workload_count = %#v, want 4", got)
	}
	if got := eco["dependency_count"]; got != float64(7) {
		t.Fatalf("ecosystem_map.dependency_count = %#v, want 7", got)
	}
	if got := data["scope"]; got != "repository" {
		t.Fatalf("scope = %#v, want repository", got)
	}
	if _, present := data["note"]; present {
		t.Fatalf("repo-scoped packet should not carry the needs-repo note; got %#v", data["note"])
	}
}

// TestGraphSummaryPacketRepoScopedOutOfGrantReturnsNotFound proves the #5167
// grant check on the repo-scoped branch: the handler runs hot-entity ranking,
// relationship counts, and the repo ecosystem map anchored entirely on the
// caller-supplied repo_id with no store-level filter of its own, so a scoped
// caller whose repo_id is outside its grant must get not_found -- with no
// graph read at all -- rather than another tenant's repo data.
func TestGraphSummaryPacketRepoScopedOutOfGrantReturnsNotFound(t *testing.T) {
	t.Parallel()

	var captured []string
	reader := &graphSummaryRecordingReader{
		recordOnly: &captured,
		single: func(string, map[string]any) (map[string]any, error) {
			return map[string]any{"count": int64(999)}, nil
		},
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := strings.NewReader(`{"repo_id":"repo-tenant-b","limit":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"}, // does not grant repo-tenant-b
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(captured) != 0 {
		t.Fatalf("graph received %d calls, want 0 for an out-of-grant repo_id", len(captured))
	}
}

// TestGraphSummaryPacketRepoScopedInGrantReturnsRealRowData is the paired
// positive case, reusing the same fixture shape as
// TestGraphSummaryPacketRepoScopedShapeIsBoundedAndDeterministic: a scoped
// caller whose repo_id IS granted reaches the graph and gets real row data
// back, proving the #5167 check is additive rather than a blanket denial.
func TestGraphSummaryPacketRepoScopedInGrantReturnsRealRowData(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		single: func(cypher string, params map[string]any) (map[string]any, error) {
			if got, want := params["repo_id"], "repo-tenant-a"; got != want {
				t.Fatalf("repo_id param = %#v, want %#v in %s", got, want, cypher)
			}
			if strings.Contains(cypher, "[r:CALLS]") {
				return map[string]any{"count": int64(40)}, nil
			}
			return map[string]any{"count": int64(0)}, nil
		},
		multi: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "total_degree") {
				return []map[string]any{
					{"function_id": "fn-a", "function_name": "Alpha", "file_path": "a.go", "incoming_calls": int64(10), "outgoing_calls": int64(5), "total_degree": int64(15)},
				}, nil
			}
			return nil, nil
		},
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := strings.NewReader(`{"repo_id":"repo-tenant-a","limit":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeGraphSummaryData(t, w.Body.Bytes())
	hot := data["hot_entities"].([]any)
	if len(hot) != 1 {
		t.Fatalf("hot_entities len = %d, want 1; body = %s", len(hot), w.Body.String())
	}
	first := hot[0].(map[string]any)
	if got, want := first["function_name"], "Alpha"; got != want {
		t.Fatalf("hot_entities[0].function_name = %#v, want %#v (real row data)", got, want)
	}
}

func TestGraphSummaryPacketWithoutRepoReturnsEcosystemCountsAndNote(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		single: func(cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(r:Repository)"):
				return map[string]any{"c": int64(9)}, nil
			case strings.Contains(cypher, "(w:Workload)"):
				return map[string]any{"c": int64(3)}, nil
			case strings.Contains(cypher, "(p:Platform)"):
				return map[string]any{"c": int64(1)}, nil
			case strings.Contains(cypher, "WorkloadInstance"):
				return map[string]any{"c": int64(0)}, nil
			}
			return nil, nil
		},
		multi: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "total_degree") {
				t.Fatalf("no-repo packet must not run a hot-entity degree scan:\n%s", cypher)
			}
			return nil, nil
		},
	}

	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}

	data := decodeGraphSummaryData(t, w.Body.Bytes())
	if got := data["scope"]; got != "ecosystem" {
		t.Fatalf("scope = %#v, want ecosystem", got)
	}
	eco := data["ecosystem_map"].(map[string]any)
	if got := eco["repo_count"]; got != float64(9) {
		t.Fatalf("ecosystem_map.repo_count = %#v, want 9", got)
	}
	if _, present := data["hot_entities"]; present {
		t.Fatalf("no-repo packet must not include hot_entities; got %#v", data["hot_entities"])
	}
	note, ok := data["note"].(string)
	if !ok || !strings.Contains(strings.ToLower(note), "repo_id") {
		t.Fatalf("note = %#v, want a string mentioning repo_id", data["note"])
	}
}

func TestGraphSummaryPacketEmptyGraphReturnsZerosNotError(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		single: func(_ string, _ map[string]any) (map[string]any, error) { return nil, nil },
		multi:  func(_ string, _ map[string]any) ([]map[string]any, error) { return nil, nil },
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{"repo_id":"repo-empty"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	data := decodeGraphSummaryData(t, w.Body.Bytes())
	hot, ok := data["hot_entities"].([]any)
	if !ok {
		t.Fatalf("hot_entities type = %T, want []any (empty slice, not null)", data["hot_entities"])
	}
	if len(hot) != 0 {
		t.Fatalf("hot_entities len = %d, want 0 for empty graph", len(hot))
	}
	rel := data["key_relationships"].(map[string]any)
	if got := rel["CALLS"]; got != float64(0) {
		t.Fatalf("key_relationships[CALLS] = %#v, want 0", got)
	}
	eco := data["ecosystem_map"].(map[string]any)
	if got := eco["file_count"]; got != float64(0) {
		t.Fatalf("ecosystem_map.file_count = %#v, want 0", got)
	}
}

func TestGraphSummaryPacketHonorsLimitTruncation(t *testing.T) {
	t.Parallel()

	var seenLimit any
	reader := &graphSummaryRecordingReader{
		single: func(_ string, _ map[string]any) (map[string]any, error) {
			return map[string]any{"count": int64(0)}, nil
		},
		multi: func(cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "total_degree") {
				seenLimit = params["limit"]
				// Return limit+1 rows to force truncation at the requested limit of 2.
				return []map[string]any{
					{"function_id": "fn-1", "function_name": "One", "total_degree": int64(9)},
					{"function_id": "fn-2", "function_name": "Two", "total_degree": int64(8)},
					{"function_id": "fn-3", "function_name": "Three", "total_degree": int64(7)},
				}, nil
			}
			return nil, nil
		},
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{"repo_id":"repo-1","limit":2}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	if seenLimit != 3 {
		t.Fatalf("hot-entity query limit param = %#v, want 3 (limit+1 for truncation probe)", seenLimit)
	}
	data := decodeGraphSummaryData(t, w.Body.Bytes())
	hot := data["hot_entities"].([]any)
	if len(hot) != 2 {
		t.Fatalf("hot_entities len = %d, want 2 (truncated to limit)", len(hot))
	}
	if got := data["hot_entities_truncated"]; got != true {
		t.Fatalf("hot_entities_truncated = %#v, want true", got)
	}
}

func TestGraphSummaryPacketTruthEnvelopePresent(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		single: func(_ string, _ map[string]any) (map[string]any, error) {
			return map[string]any{"count": int64(0), "c": int64(0)}, nil
		},
		multi: func(_ string, _ map[string]any) ([]map[string]any, error) { return nil, nil },
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{"repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/eshu.envelope+json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	truth, ok := envelope["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth envelope missing; body=%s", w.Body.String())
	}
	if got := truth["capability"]; got != "platform_impact.graph_summary_packet" {
		t.Fatalf("truth.capability = %#v, want platform_impact.graph_summary_packet", got)
	}
}

func TestGraphSummaryPacketUnsupportedOnLocalLightweightProfile(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		single: func(_ string, _ map[string]any) (map[string]any, error) {
			t.Fatal("graph summary packet must not query the graph when the capability is unsupported")
			return nil, nil
		},
		multi: func(_ string, _ map[string]any) ([]map[string]any, error) {
			t.Fatal("graph summary packet must not query the graph when the capability is unsupported")
			return nil, nil
		},
	}
	handler := &InfraHandler{Profile: ProfileLocalLightweight, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ecosystem/graph-summary", strings.NewReader(`{"repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/eshu.envelope+json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported_capability") {
		t.Fatalf("body = %s, want unsupported_capability", w.Body.String())
	}
}
