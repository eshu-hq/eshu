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
	"time"
)

func TestValidateReadOnlyCypher_RejectsMutationKeywords(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"CREATE", "CREATE (n:Node {name: 'test'})"},
		{"MERGE", "MERGE (n:Node {name: 'test'})"},
		{"DELETE", "MATCH (n) DELETE n"},
		{"DETACH", "MATCH (n) DETACH DELETE n"},
		{"SET", "MATCH (n) SET n.name = 'x'"},
		{"REMOVE", "MATCH (n) REMOVE n.name"},
		{"DROP", "DROP INDEX my_index"},
		{"CALL", "CALL db.labels()"},
		{"CALL newline separator", "CALL\ndb.labels()"},
		{"CALL block comment separator", "CALL/**/db.labels()"},
		{"FOREACH", "FOREACH (x IN [1,2] | CREATE (n))"},
		{"LOAD CSV", "LOAD CSV FROM 'file:///x' AS row"},
		{"SET newline separator", "MATCH (n) SET\nn.name = 'x'"},
		{"SET block comment separator", "MATCH (n) SET/**/n.name = 'x'"},
		{"lowercase create", "match (n) create (m)"},
		{"mixed case", "Match (n) Merge (m)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReadOnlyCypher(tc.query)
			if err == nil {
				t.Errorf("expected rejection for query %q", tc.query)
			}
		})
	}
}

func TestValidateReadOnlyCypher_AllowsReadOnlyQueries(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"simple match", "MATCH (n) RETURN n LIMIT 10"},
		{"count nodes", "MATCH (n) RETURN count(n)"},
		{"relationship query", "MATCH (a)-[r]->(b) RETURN a, r, b"},
		{"with clause", "MATCH (n) WITH n.name AS name RETURN name"},
		{"optional match", "OPTIONAL MATCH (n) RETURN n"},
		{"where clause", "MATCH (n) WHERE n.name = 'test' RETURN n"},
		{"order by", "MATCH (n) RETURN n ORDER BY n.name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReadOnlyCypher(tc.query)
			if err != nil {
				t.Errorf("expected valid query %q to pass, got: %v", tc.query, err)
			}
		})
	}
}

func TestValidateReadOnlyCypher_RejectsLongQueries(t *testing.T) {
	long := strings.Repeat("MATCH (n) RETURN n ", 300)
	err := validateReadOnlyCypher(long)
	if err == nil {
		t.Error("expected rejection for excessively long query")
	}
}

func TestHandleCypherQuery_RejectsMutations(t *testing.T) {
	h := &CodeHandler{Neo4j: &stubGraphReader{}}

	body := `{"cypher_query": "CREATE (n:Node) RETURN n"}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCypherQueryRejectsMutationWithEnvelopeError(t *testing.T) {
	t.Parallel()

	h := &CodeHandler{Neo4j: &stubGraphReader{}}

	body := `{"cypher_query": "CALL/**/db.labels()"}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid envelope JSON: %v", err)
	}
	if envelope.Error == nil {
		t.Fatalf("envelope error = nil, want invalid argument error")
	}
	if got, want := envelope.Error.Code, ErrorCodeInvalidArgument; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Capability, "graph_query.read_only_cypher"; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
}

func TestHandleCypherQueryRejectsUnsupportedProfileBeforeGraph(t *testing.T) {
	t.Parallel()

	stub := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			t.Fatal("graph reader was called for unsupported profile")
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub, Profile: ProfileLocalLightweight}

	body := `{"cypher_query": "MATCH (n) RETURN n", "limit": 1}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid envelope JSON: %v", err)
	}
	if envelope.Error == nil {
		t.Fatalf("envelope error = nil, want unsupported capability error")
	}
	if got, want := envelope.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Capability, "graph_query.read_only_cypher"; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
	if envelope.Error.Profiles == nil {
		t.Fatalf("error profiles = nil, want current and required profiles")
	}
	if got, want := envelope.Error.Profiles.Current, ProfileLocalLightweight; got != want {
		t.Fatalf("current profile = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Profiles.Required, ProfileLocalAuthoritative; got != want {
		t.Fatalf("required profile = %q, want %q", got, want)
	}
}

func TestHandleCypherQuery_ExecutesReadOnlyQuery(t *testing.T) {
	stub := &stubGraphReader{
		rows: []map[string]any{{"count": 42}},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n) RETURN count(n) AS count"}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Errorf("expected 1 result row, got %v", resp["results"])
	}
}

func TestHandleCypherQueryPassesDeadlineToGraph(t *testing.T) {
	t.Parallel()

	stub := fakeGraphReader{
		run: func(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatalf("graph context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > cypherQueryTimeout {
				t.Fatalf("deadline remaining = %s, want within %s", remaining, cypherQueryTimeout)
			}
			return []map[string]any{{"count": 42}}, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n) RETURN count(n) AS count"}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCypherQueryAddsBoundedLimitAndEnvelope(t *testing.T) {
	t.Parallel()

	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "LIMIT 3") {
				t.Fatalf("cypher = %q, want bounded LIMIT 3 probe", cypher)
			}
			if len(params) != 0 {
				t.Fatalf("params = %#v, want none for direct cypher", params)
			}
			return []map[string]any{{"name": "a"}, {"name": "b"}, {"name": "c"}}, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n) RETURN n.name AS name", "limit": 2}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid envelope JSON: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %#v, want two trimmed rows", data["results"])
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := envelope.Truth.Capability, "graph_query.read_only_cypher"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
}

func TestHandleCypherQueryRejectsExplicitLimitAboveRequestedLimit(t *testing.T) {
	t.Parallel()

	h := &CodeHandler{Neo4j: &stubGraphReader{}}
	body := `{"cypher_query": "MATCH (n) RETURN n LIMIT 500", "limit": 25}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "exceeds requested limit") {
		t.Fatalf("body = %s, want requested-limit rejection", w.Body.String())
	}
}

func TestHandleCypherQueryIgnoresLimitInsideStringLiteral(t *testing.T) {
	t.Parallel()

	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "LIMIT 3") {
				t.Fatalf("cypher = %q, want appended LIMIT 3 probe", cypher)
			}
			if len(params) != 0 {
				t.Fatalf("params = %#v, want none for direct cypher", params)
			}
			return []map[string]any{{"name": "LIMIT"}}, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n) WHERE n.name = 'LIMIT' RETURN n.name AS name", "limit": 2}`
	req := httptest.NewRequest("POST", "/api/v0/code/cypher", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleCypherQuery(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestOpenAPICypherRouteDocumentsUnsupportedProfile(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/code/cypher")
	post := mustMapField(t, path, "post")
	responses := mustMapField(t, post, "responses")
	if _, ok := responses["501"]; !ok {
		t.Fatalf("Cypher OpenAPI responses missing 501 unsupported profile response")
	}
}

func TestOpenAPICypherRouteDocumentsBoundedGraphReadFailures(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/code/cypher")
	post := mustMapField(t, path, "post")
	responses := mustMapField(t, post, "responses")
	for _, status := range []string{"503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("Cypher OpenAPI responses missing %s bounded graph-read response", status)
		}
	}
}
