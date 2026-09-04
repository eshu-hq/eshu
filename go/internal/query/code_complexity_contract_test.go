// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleComplexityListReturnsTruncationInEnvelope(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "ORDER BY complexity DESC, e.name, e.id") {
					t.Fatalf("cypher = %q, want deterministic complexity order", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "function:one", "name": "one", "labels": []any{"Function"}, "complexity": int64(13)},
					{"id": "function:two", "name": "two", "labels": []any{"Function"}, "complexity": int64(11)},
					{"id": "function:three", "name": "three", "labels": []any{"Function"}, "complexity": int64(9)},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %#v, want two rows", data["results"])
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestHandleComplexityRejectsAmbiguousFunctionNameInEnvelope(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded candidate probe", cypher)
				}
				if got, want := params["entity_name"], "processPayment"; got != want {
					t.Fatalf("params[entity_name] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"id":         "function:one",
						"name":       "processPayment",
						"labels":     []any{"Function"},
						"file_path":  "src/payments.ts",
						"repo_id":    "repo-1",
						"language":   "typescript",
						"start_line": int64(10),
						"end_line":   int64(20),
					},
					{
						"id":         "function:two",
						"name":       "processPayment",
						"labels":     []any{"Function"},
						"file_path":  "src/billing.ts",
						"repo_id":    "repo-1",
						"language":   "typescript",
						"start_line": int64(30),
						"end_line":   int64(40),
					},
					{
						"id":         "function:three",
						"name":       "processPayment",
						"labels":     []any{"Function"},
						"file_path":  "src/checkout.ts",
						"repo_id":    "repo-1",
						"language":   "typescript",
						"start_line": int64(50),
						"end_line":   int64(60),
					},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"function_name":"processPayment","repo_id":"repo-1"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeAmbiguous {
		t.Fatalf("envelope error = %#v, want ambiguous", envelope.Error)
	}
	if got, want := envelope.Error.Details["status"], "ambiguous"; got != want {
		t.Fatalf("error.details.status = %#v, want %#v", got, want)
	}
	candidates, ok := envelope.Error.Details["candidates"].([]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("error.details.candidates = %#v, want two candidates", envelope.Error.Details["candidates"])
	}
	if got, want := envelope.Error.Details["truncated"], true; got != want {
		t.Fatalf("error.details.truncated = %#v, want %#v", got, want)
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("candidate type = %T, want map", candidates[0])
	}
	if got, want := first["handle"], "entity:function:two"; got != want {
		t.Fatalf("first candidate handle = %#v, want %#v", got, want)
	}
}

// TestHandleComplexityFallsBackToFunctionNameAfterStaleEntityID pins the case
// the fallback exists for: an unrestricted id lookup that searched every
// repository and found nothing has proved the id stale, so the function name
// the caller also sent answers instead. A request that binds the lookup to a
// repository proves no such thing and gets not-found; see
// TestHandleComplexityRepoAnchoredEntityIDDoesNotFallBackToName.
func TestHandleComplexityFallsBackToFunctionNameAfterStaleEntityID(t *testing.T) {
	t.Parallel()

	var idCalls, nameCalls int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, params map[string]any) (map[string]any, error) {
				idCalls++
				if got, want := params["entity_id"], "function:stale"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				return nil, nil
			},
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				nameCalls++
				if got, want := params["entity_name"], "handler"; got != want {
					t.Fatalf("params[entity_name] = %#v, want %#v", got, want)
				}
				if _, ok := params["repo_id"]; ok {
					t.Fatalf("params = %#v, want no repo_id: the fallback only runs when the caller bound the lookup to no repository", params)
				}
				return []map[string]any{{
					"id":        "function:handler",
					"name":      "handler",
					"labels":    []any{"Function"},
					"repo_id":   "repo-1",
					"repo_name": "payments",
				}}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function:stale","function_name":"handler"}`),
	)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got, want := idCalls, 1; got != want {
		t.Fatalf("ID lookup calls = %d, want %d", got, want)
	}
	if got, want := nameCalls, 1; got != want {
		t.Fatalf("name lookup calls = %d, want %d", got, want)
	}
}

func TestHandleComplexityDoesNotTreatStaleEntityIDAsFunctionName(t *testing.T) {
	t.Parallel()

	var idCalls int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, params map[string]any) (map[string]any, error) {
				idCalls++
				if got, want := params["entity_id"], "function:stale"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				return nil, nil
			},
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("unexpected name lookup with params %#v", params)
				return nil, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function:stale"}`),
	)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got, want := idCalls, 1; got != want {
		t.Fatalf("ID lookup calls = %d, want %d", got, want)
	}
}

// TestHandleComplexityRepoAnchoredEntityIDDoesNotFallBackToName is the case a
// name fallback must not answer. The caller names an entity id, a function
// name, and a repository. The id belongs to a different repository, so the
// repository-anchored lookup returns nothing -- and the route's contract for
// that request is 404, not a same-named function from the repository asked
// about. Falling back would hand an exact-id caller another entity's metrics.
func TestHandleComplexityRepoAnchoredEntityIDDoesNotFallBackToName(t *testing.T) {
	t.Parallel()

	var idCalls int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, params map[string]any) (map[string]any, error) {
				idCalls++
				if got, want := params["repo_id"], "repo-a"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return nil, nil
			},
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("name lookup ran for a repository-anchored entity id: %#v", params)
				return nil, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function:in-repo-b","function_name":"handler","repo_id":"repo-a"}`),
	)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got, want := idCalls, 1; got != want {
		t.Fatalf("ID lookup calls = %d, want %d", got, want)
	}
	if strings.Contains(rec.Body.String(), "complexity") {
		t.Fatalf("a not-found answer carried metrics: %s", rec.Body.String())
	}
}

// TestHandleComplexityScopedEntityIDDoesNotFallBackToName is the same rule for
// the other repository binding. A scoped caller's grant filters the id lookup
// exactly as a supplied repo_id does, so an empty result is again ambiguous
// between "stale id" and "id held elsewhere", and the answer stays not-found.
func TestHandleComplexityScopedEntityIDDoesNotFallBackToName(t *testing.T) {
	t.Parallel()

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	captured, status := captureCodeQualityCypher(
		t,
		"/api/v0/code/complexity",
		map[string]any{"entity_id": "function:in-another-tenant", "function_name": "handler"},
		&auth,
	)
	if got, want := status, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if statement, _, ok := captured.matching("$entity_name"); ok {
		t.Fatalf("name lookup ran for a grant-filtered entity id:\n%s", statement)
	}
}
