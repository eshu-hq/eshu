// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// threeIncomingCallersHandler returns a CodeHandler whose graph reader always
// answers with three bounded incoming CALLS rows, so budget and multi-type
// behavior can be asserted deterministically.
func threeIncomingCallersHandler() *CodeHandler {
	return &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{"direction": "incoming", "type": "CALLS", "source_id": "fn-a", "source_name": "alpha", "target_id": "fn-t", "target_name": "target"},
					{"direction": "incoming", "type": "CALLS", "source_id": "fn-b", "source_name": "bravo", "target_id": "fn-t", "target_name": "target"},
					{"direction": "incoming", "type": "CALLS", "source_id": "fn-c", "source_name": "charlie", "target_id": "fn-t", "target_name": "target"},
				}, nil
			},
		},
	}
}

func postRelationshipStory(t *testing.T, handler *CodeHandler, body string) map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v body=%s", err, w.Body.String())
	}
	return resp
}

func TestRelationshipStoryWithoutTokenBudgetOmitsBudgetAccounting(t *testing.T) {
	t.Parallel()

	resp := postRelationshipStory(t, threeIncomingCallersHandler(),
		`{"entity_id":"fn-t","relationship_type":"CALLS","direction":"incoming","limit":10}`)

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if got, want := len(relationships), 3; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	summary, ok := resp["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary type = %T, want map[string]any", resp["summary"])
	}
	if _, present := summary["token_budget"]; present {
		t.Fatalf("summary.token_budget must be absent when token_budget is unset, got %#v", summary["token_budget"])
	}
}

func TestRelationshipStoryTokenBudgetTooSmallDropsAllAndExplains(t *testing.T) {
	t.Parallel()

	// token_budget=1 is below the cost of even a single row, so every row is
	// dropped and the response must explain the cut rather than over-fetch.
	resp := postRelationshipStory(t, threeIncomingCallersHandler(),
		`{"entity_id":"fn-t","relationship_type":"CALLS","direction":"incoming","limit":10,"token_budget":1}`)

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if got, want := len(relationships), 0; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	summary := resp["summary"].(map[string]any)
	budget, ok := summary["token_budget"].(map[string]any)
	if !ok {
		t.Fatalf("summary.token_budget type = %T, want map[string]any", summary["token_budget"])
	}
	if got, want := budget["truncated"], true; got != want {
		t.Fatalf("token_budget.truncated = %#v, want %#v", got, want)
	}
	if got, want := intFromJSON(budget["dropped"]), 3; got != want {
		t.Fatalf("token_budget.dropped = %#v, want %d", budget["dropped"], want)
	}
	if got, want := intFromJSON(budget["available_before_budget"]), 3; got != want {
		t.Fatalf("token_budget.available_before_budget = %#v, want %d", budget["available_before_budget"], want)
	}
	if got, want := intFromJSON(budget["limit"]), 1; got != want {
		t.Fatalf("token_budget.limit = %#v, want %d", budget["limit"], want)
	}
	guidance, _ := budget["guidance"].(string)
	if strings.TrimSpace(guidance) == "" {
		t.Fatalf("token_budget.guidance must teach the agent how to narrow, got %q", guidance)
	}
}

func TestRelationshipStoryGenerousTokenBudgetKeepsAllRows(t *testing.T) {
	t.Parallel()

	resp := postRelationshipStory(t, threeIncomingCallersHandler(),
		`{"entity_id":"fn-t","relationship_type":"CALLS","direction":"incoming","limit":10,"token_budget":100000}`)

	relationships := resp["relationships"].([]any)
	if got, want := len(relationships), 3; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	budget := resp["summary"].(map[string]any)["token_budget"].(map[string]any)
	if got, want := budget["truncated"], false; got != want {
		t.Fatalf("token_budget.truncated = %#v, want %#v", got, want)
	}
	if got, want := intFromJSON(budget["dropped"]), 0; got != want {
		t.Fatalf("token_budget.dropped = %#v, want %d", budget["dropped"], want)
	}
	if estimated := intFromJSON(budget["estimated_tokens"]); estimated <= 0 {
		t.Fatalf("token_budget.estimated_tokens = %#v, want > 0", budget["estimated_tokens"])
	}
}

func TestRelationshipStoryNegativeTokenBudgetRejected(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	threeIncomingCallersHandler().Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"fn-t","direction":"incoming","token_budget":-5}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestRelationshipStoryMultipleRelationshipTypesQueryEachType(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		seen []string
	)
	record := func(relationshipType string) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, relationshipType)
	}
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, ":CALLS"):
					record("CALLS")
					return []map[string]any{
						{"direction": "incoming", "type": "CALLS", "source_id": "fn-a", "source_name": "alpha", "target_id": "fn-t", "target_name": "target"},
					}, nil
				case strings.Contains(cypher, ":IMPORTS"):
					record("IMPORTS")
					return []map[string]any{
						{"direction": "incoming", "type": "IMPORTS", "source_id": "mod-x", "source_name": "modx", "target_id": "fn-t", "target_name": "target"},
					}, nil
				default:
					t.Fatalf("unexpected cypher relationship type: %q", cypher)
					return nil, nil
				}
			},
		},
	}

	resp := postRelationshipStory(t, handler,
		`{"entity_id":"fn-t","relationship_types":["CALLS","IMPORTS"],"direction":"incoming","limit":10}`)

	sort.Strings(seen)
	if got, want := strings.Join(seen, ","), "CALLS,IMPORTS"; got != want {
		t.Fatalf("queried relationship types = %q, want %q", got, want)
	}
	relationships := resp["relationships"].([]any)
	if got, want := len(relationships), 2; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	types, ok := coverage["relationship_types"].([]any)
	if !ok {
		t.Fatalf("coverage.relationship_types type = %T, want []any", coverage["relationship_types"])
	}
	if got, want := len(types), 2; got != want {
		t.Fatalf("coverage.relationship_types = %#v, want 2 entries", coverage["relationship_types"])
	}
}

func TestRelationshipStoryRelationshipTypesRejectedWithTransitive(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	threeIncomingCallersHandler().Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"fn-t","relationship_types":["CALLS","IMPORTS"],"direction":"incoming","include_transitive":true}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestRelationshipStoryUnsupportedRelationshipTypeRejected(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	threeIncomingCallersHandler().Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"fn-t","relationship_types":["CALLS","BOGUS"],"direction":"incoming"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

// intFromJSON normalizes a JSON number (decoded as float64) to an int for
// assertions.
func intFromJSON(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return -1
	}
}
