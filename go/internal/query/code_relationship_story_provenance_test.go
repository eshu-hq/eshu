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

// TestHandleRelationshipStorySurfacesEdgeProvenance proves the relationship
// story answer carries per-edge confidence and resolution_method (ADR #2222)
// and omits both for a legacy edge that has no recorded provenance.
func TestHandleRelationshipStorySurfacesEdgeProvenance(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "rel.confidence as confidence") ||
					!strings.Contains(cypher, "rel.resolution_method as resolution_method") {
					t.Fatalf("cypher = %q, want per-edge confidence and resolution_method", cypher)
				}
				return []map[string]any{
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-scip-caller",
						"source_name":       "scipCaller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.99,
						"resolution_method": "scip",
					},
					{
						// Legacy edge with no recorded provenance.
						"direction":   "incoming",
						"type":        "CALLS",
						"source_id":   "function-legacy-caller",
						"source_name": "legacyCaller",
						"target_id":   "function-target",
						"target_name": "process_payment",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":5}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	resp := decodeRelationshipStoryTestResponse(t, w)
	relationships := relationshipStoryTestRows(t, resp)
	if got, want := len(relationships), 2; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}

	scip := relationships[0].(map[string]any)
	if got, want := scip["resolution_method"], "scip"; got != want {
		t.Fatalf("relationships[0].resolution_method = %#v, want %#v", got, want)
	}
	if got, want := scip["confidence"], 0.99; got != want {
		t.Fatalf("relationships[0].confidence = %#v, want %#v", got, want)
	}

	legacy := relationships[1].(map[string]any)
	if _, present := legacy["resolution_method"]; present {
		t.Fatalf("legacy edge should omit resolution_method, got %#v", legacy["resolution_method"])
	}
	if _, present := legacy["confidence"]; present {
		t.Fatalf("legacy edge should omit confidence, got %#v", legacy["confidence"])
	}
}

func TestHandleRelationshipStorySurfacesRelationshipProvenanceBlock(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-caller",
						"source_name":       "caller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.94,
						"resolution_method": "parser_call_expression",
						"reason":            "direct static call expression",
					},
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-target",
						"source_name":       "process_payment",
						"target_id":         "service-payments",
						"target_name":       "payments-service",
						"confidence":        0.71,
						"confidence_basis":  "evidence_aggregate",
						"resolution_source": "inferred",
						"reason":            "two reducer facts agree",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":5}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	resp := decodeRelationshipStoryTestResponse(t, w)
	relationships := relationshipStoryTestRows(t, resp)
	if got, want := len(relationships), 2; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}

	codeProvenance := relationshipStoryTestProvenance(t, relationships[0])
	if got, want := codeProvenance["confidence"], float64(0.94); got != want {
		t.Fatalf("code provenance.confidence = %#v, want %#v", got, want)
	}
	if got, want := codeProvenance["method"], "parser_call_expression"; got != want {
		t.Fatalf("code provenance.method = %#v, want %#v", got, want)
	}
	if got, want := codeProvenance["source_family"], "code_edge"; got != want {
		t.Fatalf("code provenance.source_family = %#v, want %#v", got, want)
	}
	if got, want := codeProvenance["truth_state"], "derived"; got != want {
		t.Fatalf("code provenance.truth_state = %#v, want %#v", got, want)
	}
	if got, want := codeProvenance["derived"], true; got != want {
		t.Fatalf("code provenance.derived = %#v, want %#v", got, want)
	}

	correlationProvenance := relationshipStoryTestProvenance(t, relationships[1])
	if got, want := correlationProvenance["method"], "evidence_aggregate"; got != want {
		t.Fatalf("correlation provenance.method = %#v, want %#v", got, want)
	}
	if got, want := correlationProvenance["source_family"], "correlation_edge"; got != want {
		t.Fatalf("correlation provenance.source_family = %#v, want %#v", got, want)
	}
	if got, want := correlationProvenance["truth_state"], "heuristic"; got != want {
		t.Fatalf("correlation provenance.truth_state = %#v, want %#v", got, want)
	}
	if got, want := correlationProvenance["heuristic"], true; got != want {
		t.Fatalf("correlation provenance.heuristic = %#v, want %#v", got, want)
	}
	if got, want := correlationProvenance["reason"], "two reducer facts agree"; got != want {
		t.Fatalf("correlation provenance.reason = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryAppliesMinConfidenceFloor(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-high-caller",
						"source_name":       "highCaller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.92,
						"resolution_method": "scip",
					},
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-low-caller",
						"source_name":       "lowCaller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.41,
						"resolution_method": "scope_unique_name",
					},
					{
						// Legacy edge with no recorded confidence must not
						// satisfy a positive floor.
						"direction":   "incoming",
						"type":        "CALLS",
						"source_id":   "function-legacy-caller",
						"source_name": "legacyCaller",
						"target_id":   "function-target",
						"target_name": "process_payment",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":5,"min_confidence":0.8}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	resp := decodeRelationshipStoryTestResponse(t, w)
	relationships := relationshipStoryTestRows(t, resp)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("len(relationships) = %d, want %d: %#v", got, want, relationships)
	}
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["source_name"], "highCaller"; got != want {
		t.Fatalf("relationships[0].source_name = %#v, want %#v", got, want)
	}
	scope := resp["scope"].(map[string]any)
	if got, want := scope["min_confidence"], 0.8; got != want {
		t.Fatalf("scope.min_confidence = %#v, want %#v", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	if got, want := coverage["min_confidence"], 0.8; got != want {
		t.Fatalf("coverage.min_confidence = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryProvenanceSurvivesMinConfidenceAndEmptyResults(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-strong",
						"source_name":       "strongCaller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.92,
						"resolution_method": "parser_call_expression",
						"reason":            "direct static call expression",
					},
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-weak",
						"source_name":       "weakCaller",
						"target_id":         "function-target",
						"target_name":       "process_payment",
						"confidence":        0.42,
						"resolution_method": "semantic_hint",
						"reason":            "weak semantic hint",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":5,"min_confidence":0.8}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	resp := decodeRelationshipStoryTestResponse(t, w)
	relationships := relationshipStoryTestRows(t, resp)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	provenance := relationshipStoryTestProvenance(t, relationships[0])
	if got, want := provenance["confidence"], float64(0.92); got != want {
		t.Fatalf("provenance.confidence = %#v, want %#v", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	if got, want := coverage["min_confidence"], float64(0.8); got != want {
		t.Fatalf("coverage.min_confidence = %#v, want %#v", got, want)
	}

	emptyResp := requestRelationshipStoryTestResponse(
		t,
		&CodeHandler{
			Neo4j: fakeGraphReader{
				run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return []map[string]any{}, nil
				},
			},
		},
		`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":5}`,
	)
	if got, want := len(relationshipStoryTestRows(t, emptyResp)), 0; got != want {
		t.Fatalf("empty len(relationships) = %d, want %d", got, want)
	}
}

func TestHandleRelationshipStoryMinConfidenceValidation(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, body := range []string{
		`{"entity_id":"function-target","min_confidence":-0.1}`,
		`{"entity_id":"function-target","min_confidence":1.01}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("status for body %s = %d, want %d body=%s", body, got, want, w.Body.String())
		}
	}
}

func requestRelationshipStoryTestResponse(
	t *testing.T,
	handler *CodeHandler,
	body string,
) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(body),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	return decodeRelationshipStoryTestResponse(t, w)
}

func decodeRelationshipStoryTestResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	return resp
}

func relationshipStoryTestRows(t *testing.T, resp map[string]any) []any {
	t.Helper()

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	return relationships
}

func relationshipStoryTestProvenance(t *testing.T, relationship any) map[string]any {
	t.Helper()

	row, ok := relationship.(map[string]any)
	if !ok {
		t.Fatalf("relationship type = %T, want map[string]any", relationship)
	}
	provenance, ok := row["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("relationship.provenance type = %T, want map[string]any", row["provenance"])
	}
	return provenance
}
