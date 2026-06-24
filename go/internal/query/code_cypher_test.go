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

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestHandleVisualizeQuery_ReturnsGraphPacket locks issue #3485: the tool must
// execute the caller's read-only Cypher and return a real renderable subgraph
// (visualization packet with nodes and edges) derived from the result, not a
// dead hardcoded localhost:7474 browser URL.
func TestHandleVisualizeQuery_ReturnsGraphPacket(t *testing.T) {
	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "LIMIT") {
				t.Fatalf("cypher = %q, want a bounded read-only query with LIMIT", cypher)
			}
			service := neo4jdriver.Node{
				ElementId: "node-service-1",
				Labels:    []string{"Service"},
				Props:     map[string]any{"name": "catalog"},
			}
			repo := neo4jdriver.Node{
				ElementId: "node-repo-1",
				Labels:    []string{"Repository"},
				Props:     map[string]any{"name": "catalog-repo"},
			}
			rel := neo4jdriver.Relationship{
				ElementId:      "rel-1",
				StartElementId: "node-service-1",
				EndElementId:   "node-repo-1",
				Type:           "DEPLOYED_FROM",
			}
			return []map[string]any{{"s": service, "r": repo, "rel": rel}}, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (s:Service)-[rel:DEPLOYED_FROM]->(r:Repository) RETURN s, r, rel LIMIT 10"}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want graph-query capability")
	}
	if got, want := envelope.Truth.Capability, "visualization.graph_query"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisAuthoritativeGraph; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}
	resp, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if _, dead := resp["url"]; dead {
		t.Fatalf("response still carries a dead browser url field: %v", resp["url"])
	}
	packet, ok := resp["visualization_packet"].(map[string]any)
	if !ok {
		t.Fatalf("resp[visualization_packet] type = %T, want map", resp["visualization_packet"])
	}
	if supported, _ := packet["supported"].(bool); !supported {
		t.Fatalf("packet supported = %v, want true: %#v", packet["supported"], packet)
	}
	nodes, ok := packet["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("packet nodes = %#v, want 2 nodes", packet["nodes"])
	}
	edges, ok := packet["edges"].([]any)
	if !ok || len(edges) != 1 {
		t.Fatalf("packet edges = %#v, want 1 edge", packet["edges"])
	}
	edge, ok := edges[0].(map[string]any)
	if !ok {
		t.Fatalf("edge type = %T, want map", edges[0])
	}
	if got, want := edge["relationship"], "DEPLOYED_FROM"; got != want {
		t.Fatalf("edge relationship = %v, want %v", got, want)
	}
}

// TestHandleVisualizeQuery_EmptyResult locks the empty-result-set contract: an
// executed query that returns no graph rows yields an explicit unsupported
// packet rather than an error or a fabricated subgraph.
func TestHandleVisualizeQuery_EmptyResult(t *testing.T) {
	stub := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n:Service) RETURN n LIMIT 10"}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	resp, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	packet, ok := resp["visualization_packet"].(map[string]any)
	if !ok {
		t.Fatalf("resp[visualization_packet] type = %T, want map", resp["visualization_packet"])
	}
	if supported, _ := packet["supported"].(bool); supported {
		t.Fatalf("packet supported = true, want false for empty result: %#v", packet)
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

// TestHandleVisualizeQuery_ErrorEnvelopeCarriesVisualizationCapability locks the
// failure-path capability contract: an invalid-Cypher rejection from the
// visualize tool must report the visualization.graph_query capability, not the
// read-only-cypher capability, so MCP/envelope clients see the error under the
// tool they actually called.
func TestHandleVisualizeQuery_ErrorEnvelopeCarriesVisualizationCapability(t *testing.T) {
	h := &CodeHandler{Neo4j: &stubGraphReader{}}

	body := `{"cypher_query": "DELETE (n)"}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

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
	if got, want := envelope.Error.Capability, "visualization.graph_query"; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
}

// TestHandleVisualizeQuery_InnerLimitGetsTerminalCap locks the bounded-execution
// contract for inner-LIMIT queries (review P2): when the caller's Cypher carries
// only a non-terminal LIMIT (here in a WITH clause), the final result set is
// unbounded, so the handler must inject a terminal LIMIT before executing.
// Otherwise the live path Collects an unbounded result before slicing in memory.
func TestHandleVisualizeQuery_InnerLimitGetsTerminalCap(t *testing.T) {
	var executed string
	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			executed = cypher
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"cypher_query": "MATCH (n) WITH n LIMIT 1 MATCH (m) RETURN m", "limit": 5}`
	req := httptest.NewRequest("POST", "/api/v0/code/visualize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleVisualizeQuery(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	// The executed query must end with a terminal LIMIT that bounds the final
	// RETURN, not just the inner WITH ... LIMIT 1.
	trimmed := strings.TrimSpace(executed)
	if !strings.HasSuffix(strings.ToUpper(trimmed), "LIMIT 6") {
		t.Fatalf("executed query = %q, want a terminal LIMIT 6 (requested 5 + 1 probe) appended after the inner WITH LIMIT", executed)
	}
}

// TestBoundedVisualizationCypher_TerminalCap verifies the terminal-cap contract
// the live visualization path depends on: queries with no terminal LIMIT (none,
// or only an inner WITH LIMIT) get a terminal LIMIT appended; queries already
// terminally bounded are honored and enforced against the requested limit.
func TestBoundedVisualizationCypher_TerminalCap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		query      string
		limit      int
		wantSuffix string // when set, executed query must end with this
		wantErr    bool
	}{
		{
			name:       "no limit appends terminal",
			query:      "MATCH (n) RETURN n",
			limit:      5,
			wantSuffix: "LIMIT 6",
		},
		{
			name:       "inner with-limit only appends terminal",
			query:      "MATCH (n) WITH n LIMIT 1 MATCH (m) RETURN m",
			limit:      5,
			wantSuffix: "LIMIT 6",
		},
		{
			name:       "terminal limit honored unchanged",
			query:      "MATCH (n) RETURN n LIMIT 3",
			limit:      5,
			wantSuffix: "LIMIT 3",
		},
		{
			name:       "inner and terminal limit keeps terminal",
			query:      "MATCH (n) WITH n LIMIT 1 MATCH (m) RETURN m LIMIT 4",
			limit:      5,
			wantSuffix: "LIMIT 4",
		},
		{
			name:    "terminal limit above requested rejected",
			query:   "MATCH (n) RETURN n LIMIT 9",
			limit:   5,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := boundedVisualizationCypher(tc.query, tc.limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("boundedVisualizationCypher(%q) error = nil, want error", tc.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("boundedVisualizationCypher(%q) error = %v, want nil", tc.query, err)
			}
			if !strings.HasSuffix(strings.ToUpper(strings.TrimSpace(got)), tc.wantSuffix) {
				t.Fatalf("boundedVisualizationCypher(%q) = %q, want suffix %q", tc.query, got, tc.wantSuffix)
			}
		})
	}
}

// TestHandleSearchBundles_SearchesRegistryPackages proves the bundle search
// queries the pre-indexed package registry catalog (:Package nodes) by package
// identity, not repository names. Regression guard for #3493: the handler used
// to run `MATCH (r:Repository) WHERE r.name CONTAINS $query`, returning repo
// names from a tool documented as a registry/SBOM bundle search.
func TestHandleSearchBundles_SearchesRegistryPackages(t *testing.T) {
	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, ":Repository") || strings.Contains(cypher, "r.repo_id") {
				t.Fatalf("cypher = %q, want package registry bundle query, not repository-name search", cypher)
			}
			if !strings.Contains(cypher, ":Package") {
				t.Fatalf("cypher = %q, want :Package registry catalog match", cypher)
			}
			if !strings.Contains(cypher, "ORDER BY") || !strings.Contains(cypher, "LIMIT $limit") {
				t.Fatalf("cypher = %q, want deterministic bounded ordering", cypher)
			}
			if got, want := params["limit"], 2; got != want {
				t.Fatalf("params[limit] = %#v, want %#v", got, want)
			}
			if got, want := params["query"], "react"; got != want {
				t.Fatalf("params[query] = %#v, want %#v", got, want)
			}
			return []map[string]any{
				{"package_id": "pkg-1", "name": "react", "ecosystem": "npm", "registry": "npmjs", "namespace": "", "purl": "pkg:npm/react@18", "version_count": int64(3)},
				{"package_id": "pkg-2", "name": "react-dom", "ecosystem": "npm", "registry": "npmjs", "namespace": "", "purl": "pkg:npm/react-dom@18", "version_count": int64(2)},
			}, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"query": "react", "limit": 1}`
	req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleSearchBundles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	resp, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	bundles, ok := resp["bundles"].([]any)
	if !ok || len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %v", resp["bundles"])
	}
	first, ok := bundles[0].(map[string]any)
	if !ok {
		t.Fatalf("bundle type = %T, want map", bundles[0])
	}
	if got, want := first["package_id"], "pkg-1"; got != want {
		t.Fatalf("bundle package_id = %#v, want %#v", got, want)
	}
	if _, hasRepoID := first["repo_id"]; hasRepoID {
		t.Fatalf("bundle leaked repo_id field: %#v", first)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

// TestHandleSearchBundles_ScopesByEcosystem proves the optional ecosystem scope
// is forwarded to the registry query so callers can bound the bundle catalog
// read to one package ecosystem (eshu-mcp-call-rigor: bounded, scoped reads).
func TestHandleSearchBundles_ScopesByEcosystem(t *testing.T) {
	stub := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "$ecosystem") {
				t.Fatalf("cypher = %q, want ecosystem scope predicate", cypher)
			}
			if got, want := params["ecosystem"], "npm"; got != want {
				t.Fatalf("params[ecosystem] = %#v, want %#v", got, want)
			}
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"query": "react", "ecosystem": "npm"}`
	req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleSearchBundles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestHandleSearchBundles_RequiresScope proves the handler rejects a request
// that supplies neither `query` nor `ecosystem` and never reaches the graph.
// Regression guard for #3506: an unscoped request used to scan and aggregate
// every :Package / version before LIMIT, violating the bounded read contract.
func TestHandleSearchBundles_RequiresScope(t *testing.T) {
	for name, body := range map[string]string{
		"empty body":        ``,
		"empty object":      `{}`,
		"blank query":       `{"query": "   "}`,
		"blank scope pair":  `{"query": "", "ecosystem": ""}`,
		"limit only":        `{"limit": 10}`,
		"unique_only":       `{"unique_only": true}`,
		"whitespace scopes": `{"query": " ", "ecosystem": "\t"}`,
	} {
		t.Run(name, func(t *testing.T) {
			queried := false
			stub := fakeGraphReader{
				run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
					queried = true
					return nil, nil
				},
			}
			h := &CodeHandler{Neo4j: stub}

			req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			w := httptest.NewRecorder()

			h.handleSearchBundles(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
			}
			if queried {
				t.Fatalf("graph was queried for an unscoped bundle request; want bounded reject before scan")
			}
		})
	}
}

// TestHandleSearchBundles_UnscopedReturnsEnvelopeError proves that when the
// caller accepts the canonical envelope (the MCP dispatch path does), an
// unscoped bundle request returns a ResponseEnvelope with a populated Error
// field rather than the plain {error, detail} body. #3520 follow-up: a
// non-envelope 400 on the MCP path is not recognized as a canonical envelope,
// so it degrades to a transport error instead of a structured IsError result.
func TestHandleSearchBundles_UnscopedReturnsEnvelopeError(t *testing.T) {
	queried := false
	stub := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			queried = true
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleSearchBundles(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if queried {
		t.Fatalf("graph was queried for an unscoped request; want bounded reject before scan")
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if envelope.Error == nil {
		t.Fatalf("envelope.Error = nil, want populated error envelope; body=%s", w.Body.String())
	}
	if got, want := envelope.Error.Code, ErrorCodeInvalidArgument; got != want {
		t.Fatalf("envelope.Error.Code = %q, want %q", got, want)
	}
	if envelope.Error.Capability == "" {
		t.Fatalf("envelope.Error.Capability is empty, want bundle search capability")
	}
}

// TestSearchRegistryBundlesCypherAlwaysScoped proves the query builder never
// emits a bare whole-catalog scan: every produced query carries a selective
// predicate ($query or $ecosystem) ahead of the version aggregation, so a
// catalog-head scan-and-aggregate is impossible by construction.
func TestSearchRegistryBundlesCypherAlwaysScoped(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		ecosystem string
	}{
		{"query only", "react", ""},
		{"ecosystem only", "", "npm"},
		{"both", "react", "npm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cypher, params := searchRegistryBundlesCypher(tc.query, tc.ecosystem, false, 51)
			hasQueryPredicate := strings.Contains(cypher, "$query")
			hasEcosystemPredicate := strings.Contains(cypher, "$ecosystem")
			if !hasQueryPredicate && !hasEcosystemPredicate {
				t.Fatalf("cypher = %q, want at least one selective predicate", cypher)
			}
			if tc.query != "" && params["query"] != tc.query {
				t.Fatalf("params[query] = %#v, want %#v", params["query"], tc.query)
			}
			if tc.ecosystem != "" && params["ecosystem"] != tc.ecosystem {
				t.Fatalf("params[ecosystem] = %#v, want %#v", params["ecosystem"], tc.ecosystem)
			}
		})
	}
}

// TestHandleSearchBundles_EmptyResults proves the handler returns a bounded,
// non-truncated empty envelope rather than erroring when no bundles match.
func TestHandleSearchBundles_EmptyResults(t *testing.T) {
	stub := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
	}
	h := &CodeHandler{Neo4j: stub}

	body := `{"query": "does-not-exist"}`
	req := httptest.NewRequest("POST", "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	h.handleSearchBundles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	resp, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if got, want := resp["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
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
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
				return []map[string]any{{
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
				}}, nil
			default:
				t.Fatalf("unexpected Run call %d", calls)
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
		t.Fatalf("Run call count = %d, want %d", got, want)
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
				if got, want := params["limit"], 3; got != want {
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
