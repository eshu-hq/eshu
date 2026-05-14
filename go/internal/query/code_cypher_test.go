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
		{"FOREACH", "FOREACH (x IN [1,2] | CREATE (n))"},
		{"LOAD CSV", "LOAD CSV FROM 'file:///x' AS row"},
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

func TestHandleVisualizeQuery_ReturnsURL(t *testing.T) {
	h := &CodeHandler{Neo4j: &stubGraphReader{}}

	body := `{"cypher_query": "MATCH (n) RETURN n LIMIT 10"}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	u, ok := resp["url"].(string)
	if !ok || !strings.Contains(u, "browser") {
		t.Errorf("expected Neo4j Browser URL, got %v", resp["url"])
	}
}

func TestHandleVisualizeQuery_RejectsMutations(t *testing.T) {
	h := &CodeHandler{Neo4j: &stubGraphReader{}}

	body := `{"cypher_query": "DELETE (n)"}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSearchBundles_ReturnsResults(t *testing.T) {
	stub := &stubGraphReader{
		rows: []map[string]any{
			{"name": "my-repo", "repo_id": "abc123"},
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"query": "my-repo"}`
	req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleSearchBundles(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	bundles, ok := resp["bundles"].([]any)
	if !ok || len(bundles) != 1 {
		t.Errorf("expected 1 bundle, got %v", resp["bundles"])
	}
}

func TestHandleComplexityAcceptsFunctionNameSelector(t *testing.T) {
	t.Parallel()

	var calls int
	handler := &CodeHandler{
		Neo4j: &stubGraphReader{
			rows: nil,
		},
	}
	handler.Neo4j = fakeGraphReader{
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			calls++
			switch calls {
			case 1:
				if got, want := params["entity_name"], "search"; got != want {
					t.Fatalf("params[entity_name] = %#v, want %#v", got, want)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "e.name = $entity_name") {
					t.Fatalf("cypher = %q, want function-name lookup", cypher)
				}
				return map[string]any{
					"id":                  "function-1",
					"name":                "search",
					"labels":              []any{"Function"},
					"file_path":           "src/search.go",
					"repo_id":             "repo-1",
					"repo_name":           "catalog",
					"language":            "go",
					"start_line":          int64(8),
					"end_line":            int64(21),
					"outgoing_count":      int64(2),
					"incoming_count":      int64(1),
					"total_relationships": int64(3),
				}, nil
			default:
				t.Fatalf("unexpected RunSingle call %d", calls)
				return nil, nil
			}
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"function_name":"search","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := calls, 1; got != want {
		t.Fatalf("RunSingle call count = %d, want %d", got, want)
	}
}

func TestHandleComplexityListsMostComplexFunctionsWhenSelectorOmitted(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (e:Function)") {
					t.Fatalf("cypher = %q, want function-only complexity listing", cypher)
				}
				if !strings.Contains(cypher, "ORDER BY complexity DESC") {
					t.Fatalf("cypher = %q, want descending complexity order", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"id":         "function-1",
						"name":       "search",
						"labels":     []any{"Function"},
						"file_path":  "src/search.go",
						"repo_id":    "repo-1",
						"repo_name":  "catalog",
						"language":   "go",
						"start_line": int64(8),
						"end_line":   int64(21),
						"complexity": int64(13),
					},
					{
						"id":         "function-2",
						"name":       "rank",
						"labels":     []any{"Function"},
						"file_path":  "src/rank.go",
						"repo_id":    "repo-1",
						"repo_name":  "catalog",
						"language":   "go",
						"start_line": int64(5),
						"end_line":   int64(17),
						"complexity": int64(9),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
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
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("resp[results] = %#v, want 2 results", resp["results"])
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[results][0] type = %T, want map[string]any", results[0])
	}
	if got, want := first["complexity"], float64(13); got != want {
		t.Fatalf("first[complexity] = %#v, want %#v", got, want)
	}
}

// stubGraphReader is a test double for GraphReader.
type stubGraphReader struct {
	rows []map[string]any
	err  error
}

func (s *stubGraphReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return s.rows, s.err
}

func (s *stubGraphReader) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	if len(s.rows) == 0 {
		return nil, s.err
	}
	return s.rows[0], s.err
}
