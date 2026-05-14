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

func TestResolveEntityHonorsLimitAndReturnsEnvelope(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want parameterized bounded LIMIT", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "entity:one", "name": "handler", "labels": []any{"Function"}},
					{"id": "entity:two", "name": "handler", "labels": []any{"Function"}},
					{"id": "entity:three", "name": "handler", "labels": []any{"Function"}},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"handler","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

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
	entities, ok := data["entities"].([]any)
	if !ok || len(entities) != 2 {
		t.Fatalf("entities = %#v, want two rows", data["entities"])
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "code_search.fuzzy_symbol" {
		t.Fatalf("truth = %#v, want fuzzy symbol truth", envelope.Truth)
	}
}
