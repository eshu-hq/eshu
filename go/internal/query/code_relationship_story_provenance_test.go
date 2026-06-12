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
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
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
