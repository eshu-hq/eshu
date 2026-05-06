package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFilterNullRelationshipsSupportsTypedMapSlice(t *testing.T) {
	t.Parallel()

	got := filterNullRelationships([]map[string]any{
		{
			"type":        "CALLS",
			"source_name": "dispatchGraphProof",
			"source_id":   "content-entity:e_d22174df9a8f",
		},
		{
			"type": nil,
		},
	})
	if len(got) != 1 {
		t.Fatalf("len(filterNullRelationships()) = %d, want 1", len(got))
	}
	if gotValue, want := got[0]["type"], "CALLS"; gotValue != want {
		t.Fatalf("filterNullRelationships()[0][type] = %#v, want %#v", gotValue, want)
	}
}

func TestHandleRelationshipsPreservesIncomingTypedMapSlice(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "content-entity:e_9f63fe5851f8",
					"name":       "persistGraphProof",
					"labels":     []map[string]any{},
					"file_path":  "flow.go",
					"repo_id":    "repository:r_d556f935",
					"repo_name":  "graph-analysis-go",
					"language":   "go",
					"start_line": int64(11),
					"end_line":   int64(13),
					"outgoing":   []map[string]any{},
					"incoming": []map[string]any{
						{
							"direction":   "incoming",
							"type":        "CALLS",
							"source_name": "dispatchGraphProof",
							"source_id":   "content-entity:e_d22174df9a8f",
						},
					},
				}, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"persistGraphProof","repo_id":"repository:r_d556f935","direction":"incoming","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	incoming, ok := resp["incoming"].([]any)
	if !ok {
		t.Fatalf("resp[incoming] type = %T, want []any", resp["incoming"])
	}
	if len(incoming) != 1 {
		t.Fatalf("len(resp[incoming]) = %d, want 1", len(incoming))
	}
	relationship, ok := incoming[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[incoming][0] type = %T, want map[string]any", incoming[0])
	}
	if got, want := relationship["source_name"], "dispatchGraphProof"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
}
