// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// TestGetRelationshipEdgesShortCircuitsUnstampedVerb proves a source_tool filter
// on a verb that never stamps it (CALLS is Tier-3 code) returns an empty page
// WITHOUT running the expensive source-label-anchored edge query.
func TestGetRelationshipEdgesShortCircuitsUnstampedVerb(t *testing.T) {
	t.Parallel()

	reader := &fakeRelationshipsGraphReader{edgesByVerb: map[string][]map[string]any{
		"CALLS": {{"source_id": "a", "source_name": "fnA", "target_id": "b", "target_name": "fnB"}},
	}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "calls", "source_tool": "terraform"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastParams != nil {
		t.Fatalf("edge query must not run for a source_tool filter on an unstamped verb; ran with %v", reader.lastParams)
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	edges, ok := data["edges"].([]any)
	if !ok {
		t.Fatalf("edges is not an array: %T", data["edges"])
	}
	if len(edges) != 0 {
		t.Fatalf("edges = %v, want empty page", edges)
	}
}

// digSpec walks a decoded OpenAPI document by successive map keys, failing the
// test if any key is missing or not an object.
func digSpec(t *testing.T, node any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		obj, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("digSpec: expected object at %q, got %T", key, node)
		}
		node, ok = obj[key]
		if !ok {
			t.Fatalf("digSpec: missing key %q", key)
		}
	}
	return node
}

// TestRelationshipEdgesSourceToolEnumMatchesCanonical guards against drift
// between the OpenAPI source_tool filter enum and the canonical vocabulary: the
// enum is hand-written JSON, so a token added to sourcetool.Canonical without
// updating the spec (or vice versa) must fail here rather than ship a contract
// that rejects a valid tool.
func TestRelationshipEdgesSourceToolEnumMatchesCanonical(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) = %v", err)
	}
	enumRaw := digSpec(t, spec,
		"paths", "/api/v0/relationships/edges", "post", "requestBody",
		"content", "application/json", "schema", "properties", "source_tool", "enum")
	enumList, ok := enumRaw.([]any)
	if !ok {
		t.Fatalf("source_tool enum is %T, want []any", enumRaw)
	}
	got := make([]string, len(enumList))
	for i, v := range enumList {
		got[i], _ = v.(string)
	}
	if !reflect.DeepEqual(got, sourcetool.Canonical) {
		t.Fatalf("source_tool enum drift:\n openapi = %v\n canonical = %v", got, sourcetool.Canonical)
	}
}
