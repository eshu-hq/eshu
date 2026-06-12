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

func TestHandleRelationshipStorySupportsImplementsAndInstantiates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		body             string
		wantRelationship string
		wantPattern      string
	}{
		{
			name:             "implements",
			body:             `{"entity_id":"interface-target","relationship_type":"IMPLEMENTS","direction":"incoming","limit":1}`,
			wantRelationship: "IMPLEMENTS",
			wantPattern:      "[rel:IMPLEMENTS]",
		},
		{
			name:             "instantiates",
			body:             `{"entity_id":"class-target","relationship_type":"INSTANTIATES","direction":"incoming","limit":1}`,
			wantRelationship: "INSTANTIATES",
			wantPattern:      "[rel:INSTANTIATES]",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &CodeHandler{
				Neo4j: fakeGraphReader{
					run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
						if !strings.Contains(cypher, tt.wantPattern) {
							t.Fatalf("cypher = %q, want typed relationship pattern %q", cypher, tt.wantPattern)
						}
						return []map[string]any{{
							"direction":   "incoming",
							"type":        tt.wantRelationship,
							"source_id":   "source-1",
							"source_name": "source",
							"target_id":   "target-1",
							"target_name": "target",
						}}, nil
					},
				},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			coverage := resp["coverage"].(map[string]any)
			relationshipTypes := coverage["relationship_types"].([]any)
			if len(relationshipTypes) != 1 || relationshipTypes[0] != tt.wantRelationship {
				t.Fatalf("coverage.relationship_types = %#v, want [%s]", relationshipTypes, tt.wantRelationship)
			}
		})
	}
}

func TestRelationshipCapabilityClassifiesImplementationAndInstantiation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		direction        string
		relationshipType string
		want             string
	}{
		{name: "implements", direction: "incoming", relationshipType: "IMPLEMENTS", want: "symbol_graph.inheritance"},
		{name: "incoming instantiates", direction: "incoming", relationshipType: "INSTANTIATES", want: "call_graph.direct_callers"},
		{name: "outgoing instantiates", direction: "outgoing", relationshipType: "INSTANTIATES", want: "call_graph.direct_callees"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := relationshipCapability(tt.direction, tt.relationshipType); got != tt.want {
				t.Fatalf("relationshipCapability(%q, %q) = %q, want %q", tt.direction, tt.relationshipType, got, tt.want)
			}
		})
	}
}
