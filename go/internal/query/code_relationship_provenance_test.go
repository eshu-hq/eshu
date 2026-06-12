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

func TestRelationshipGraphRowCypherProjectsResolutionProvenance(t *testing.T) {
	t.Parallel()

	cypher := relationshipGraphRowCypher("e.id = $entity_id")
	for _, fragment := range []string{
		"confidence: outgoingRel.confidence",
		"resolution_method: outgoingRel.resolution_method",
		"confidence: incomingRel.confidence",
		"resolution_method: incomingRel.resolution_method",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("relationshipGraphRowCypher() missing %q in %q", fragment, cypher)
		}
	}
}

func TestNornicDBOneHopRelationshipsCypherProjectsResolutionProvenance(t *testing.T) {
	t.Parallel()

	cypher, _ := nornicDBOneHopRelationshipsCypher("function-root", "outgoing", "CALLS", "Function", "uid")
	for _, fragment := range []string{
		"rel.confidence as confidence",
		"rel.resolution_method as resolution_method",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("nornicDBOneHopRelationshipsCypher() missing %q in %q", fragment, cypher)
		}
	}
}

func TestHandleRelationshipsPreservesGraphEdgeResolutionProvenance(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				for _, fragment := range []string{
					"resolution_method: outgoingRel.resolution_method",
					"confidence: outgoingRel.confidence",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want edge provenance projection %q", cypher, fragment)
					}
				}
				return map[string]any{
					"id":         "function-1",
					"name":       "handler",
					"labels":     []any{"Function"},
					"file_path":  "handler.go",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "go",
					"start_line": int64(8),
					"end_line":   int64(21),
					"outgoing": []map[string]any{
						{
							"direction":         "outgoing",
							"type":              "CALLS",
							"target_name":       "charge",
							"target_id":         "function-2",
							"confidence":        0.97,
							"resolution_method": "same_file",
							"reason":            "Resolved within the caller's file by lexical scope or unique name",
						},
					},
					"incoming": []map[string]any{},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1","direction":"outgoing","relationship_type":"CALLS"}`),
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
	outgoing := resp["outgoing"].([]any)
	relationship := outgoing[0].(map[string]any)
	if got, want := relationship["resolution_method"], "same_file"; got != want {
		t.Fatalf("relationship.resolution_method = %#v, want %#v", got, want)
	}
	if got, want := relationship["confidence"], 0.97; got != want {
		t.Fatalf("relationship.confidence = %#v, want %#v", got, want)
	}
}

func TestNormalizeGraphRelationshipSliceOmitsEmptyResolutionProvenance(t *testing.T) {
	t.Parallel()

	got := normalizeGraphRelationshipSlice([]map[string]any{
		{
			"direction":         "outgoing",
			"type":              "CALLS",
			"target_id":         "function-legacy",
			"confidence":        nil,
			"resolution_method": "",
			"reason":            "",
		},
	})
	if len(got) != 1 {
		t.Fatalf("len(normalizeGraphRelationshipSlice()) = %d, want 1", len(got))
	}
	for _, field := range []string{"confidence", "resolution_method", "reason"} {
		if _, ok := got[0][field]; ok {
			t.Fatalf("legacy relationship still has %q in %#v", field, got[0])
		}
	}
}

func TestRelationshipStoryGraphCypherProjectsResolutionProvenance(t *testing.T) {
	t.Parallel()

	cypher, _ := relationshipStoryGraphCypher(
		relationshipStoryRequest{EntityID: "function-1", RelationshipType: "CALLS"},
		nil,
		"incoming",
		graphEntityIDPredicate,
	)
	for _, fragment := range []string{
		"rel.confidence as confidence",
		"rel.resolution_method as resolution_method",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("relationshipStoryGraphCypher() missing %q in %q", fragment, cypher)
		}
	}
}

func TestNornicDBRelationshipStoryCypherProjectsResolutionProvenance(t *testing.T) {
	t.Parallel()

	cypher, _ := nornicDBRelationshipStoryGraphCypher(
		relationshipStoryRequest{EntityID: "function-1", RelationshipType: "CALLS"},
		"function-1",
		"Function",
		"uid",
		"outgoing",
	)
	for _, fragment := range []string{
		"rel.confidence as confidence",
		"rel.resolution_method as resolution_method",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("nornicDBRelationshipStoryGraphCypher() missing %q in %q", fragment, cypher)
		}
	}
}

func TestHandleRelationshipStoryPreservesGraphEdgeResolutionProvenance(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				for _, fragment := range []string{
					"rel.resolution_method as resolution_method",
					"rel.confidence as confidence",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want edge provenance projection %q", cypher, fragment)
					}
				}
				return []map[string]any{
					{
						"direction":         "incoming",
						"type":              "CALLS",
						"source_id":         "function-caller",
						"source_name":       "caller",
						"target_id":         "function-target",
						"target_name":       "target",
						"confidence":        0.91,
						"resolution_method": "import_binding",
						"reason":            "Resolved by following an explicit import or package binding",
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
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":1}`),
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
	relationships := resp["relationships"].([]any)
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["resolution_method"], "import_binding"; got != want {
		t.Fatalf("relationship.resolution_method = %#v, want %#v", got, want)
	}
	if got, want := relationship["confidence"], 0.91; got != want {
		t.Fatalf("relationship.confidence = %#v, want %#v", got, want)
	}
}
