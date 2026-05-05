package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHandleDeadCodeExcludesDecoratedEntities(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/routes.py", "Function", "handler",
					int64(10), int64(14), "python", "def handler(): pass", []byte(`{"decorators":["@route"]}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-2", "repo-1", "src/payments.py", "Function", "helper",
					int64(20), int64(30), "python", "def helper(): pass", []byte(`{"decorators":["@cached"]}`),
				},
			},
		},
	})

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id":  "function-1",
						"name":       "handler",
						"labels":     []any{"Function"},
						"file_path":  "src/routes.py",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "python",
						"start_line": int64(10),
						"end_line":   int64(14),
					},
					{
						"entity_id":  "function-2",
						"name":       "helper",
						"labels":     []any{"Function"},
						"file_path":  "src/payments.py",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "python",
						"start_line": int64(20),
						"end_line":   int64(30),
					},
				}, nil
			},
		},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","exclude_decorated_with":["@route"]}`),
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
	if !ok {
		t.Fatalf("resp[results] type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(resp[results]) = %d, want %d", got, want)
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[results][0] type = %T, want map[string]any", results[0])
	}
	if got, want := result["name"], "helper"; got != want {
		t.Fatalf("result[name] = %#v, want %#v", got, want)
	}
}

func TestHandleComplexityPreservesPythonGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "function-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				if want := "e.decorators as decorators"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				if !strings.Contains(cypher, "e.docstring as docstring") {
					t.Fatalf("cypher = %q, want graph semantic projection", cypher)
				}
				return map[string]any{
					"id":                  "function-1",
					"name":                "handler",
					"labels":              []any{"Function"},
					"file_path":           "src/routes.py",
					"repo_id":             "repo-1",
					"repo_name":           "payments",
					"language":            "python",
					"start_line":          int64(10),
					"end_line":            int64(14),
					"outgoing_count":      int64(3),
					"incoming_count":      int64(1),
					"total_relationships": int64(4),
					"decorators":          []any{"@route"},
					"async":               true,
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function-1"}`),
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
	if got, want := resp["semantic_summary"], "Function handler is async and uses decorators @route."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := resp["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[python_semantics] type = %T, want map[string]any", resp["python_semantics"])
	}
	decorators, ok := pythonSemantics["decorators"].([]any)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []any", pythonSemantics["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@route]", decorators)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["async"], true; got != want {
		t.Fatalf("semantic_profile[async] = %#v, want %#v", got, want)
	}
}

func TestHandleComplexityBuildsNonConflictingCypher(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := strings.Count(cypher, " as repo_name"), 1; got != want {
					t.Fatalf("strings.Count(cypher, \" as repo_name\") = %d, want %d; cypher=%q", got, want, cypher)
				}
				for _, fragment := range []string{
					"(repo:Repository)",
					"(e)-[outgoingRel]->()",
					"()-[incomingRel]->(e)",
					"count(DISTINCT outgoingRel)",
					"count(DISTINCT incomingRel)",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want fragment %q", cypher, fragment)
					}
				}
				for _, fragment := range []string{
					"(r:Repository)",
					"(e)-[r]->()",
					"()-[r2]->(e)",
					"count(DISTINCT r)",
					"count(DISTINCT r2)",
					"e.repo_name as repo_name",
				} {
					if strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, must not contain %q", cypher, fragment)
					}
				}
				if got, want := params["entity_id"], "function-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":                  "function-1",
					"name":                "handler",
					"labels":              []any{"Function"},
					"file_path":           "src/routes.py",
					"repo_id":             "repo-1",
					"repo_name":           "payments",
					"language":            "python",
					"start_line":          int64(10),
					"end_line":            int64(14),
					"outgoing_count":      int64(1),
					"incoming_count":      int64(2),
					"total_relationships": int64(3),
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleComplexityFallsBackToNameLookupWithinRepo(t *testing.T) {
	t.Parallel()

	var calls int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				calls++
				switch calls {
				case 1:
					if got, want := params["entity_id"], "handler"; got != want {
						t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
					}
					return nil, nil
				case 2:
					if got, want := params["entity_name"], "handler"; got != want {
						t.Fatalf("params[entity_name] = %#v, want %#v", got, want)
					}
					if got, want := params["repo_id"], "repo-1"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					if !strings.Contains(cypher, "e.name = $entity_name") {
						t.Fatalf("cypher = %q, want exact-name fallback", cypher)
					}
					if !strings.Contains(cypher, "repo.id = $repo_id") {
						t.Fatalf("cypher = %q, want repo-scoped fallback", cypher)
					}
					return map[string]any{
						"id":                  "function-1",
						"name":                "handler",
						"labels":              []any{"Function"},
						"file_path":           "src/routes.py",
						"repo_id":             "repo-1",
						"repo_name":           "payments",
						"language":            "python",
						"start_line":          int64(10),
						"end_line":            int64(14),
						"outgoing_count":      int64(1),
						"incoming_count":      int64(2),
						"total_relationships": int64(3),
					}, nil
				default:
					t.Fatalf("unexpected RunSingle call %d", calls)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"handler","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := calls, 2; got != want {
		t.Fatalf("RunSingle call count = %d, want %d", got, want)
	}
}

func TestHandleComplexityPreservesTypeScriptGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "class-ts-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.type_parameters as type_parameters",
					"e.declaration_merge_group as declaration_merge_group",
					"e.declaration_merge_count as declaration_merge_count",
					"e.declaration_merge_kinds as declaration_merge_kinds",
					"e.decorators as decorators",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}
				return map[string]any{
					"id":                      "class-ts-1",
					"name":                    "Service",
					"labels":                  []any{"Class"},
					"file_path":               "src/service.ts",
					"repo_id":                 "repo-1",
					"repo_name":               "payments",
					"language":                "typescript",
					"start_line":              int64(1),
					"end_line":                int64(12),
					"outgoing_count":          int64(3),
					"incoming_count":          int64(1),
					"total_relationships":     int64(4),
					"decorators":              []any{"@sealed"},
					"type_parameters":         []any{"T"},
					"declaration_merge_group": "Service",
					"declaration_merge_count": int64(2),
					"declaration_merge_kinds": []any{"class", "namespace"},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"class-ts-1"}`),
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
	if got, want := resp["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := resp["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[typescript_semantics] type = %T, want map[string]any", resp["typescript_semantics"])
	}
	if got, want := typescriptSemantics["decorators"], []any{"@sealed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := typescriptSemantics["type_parameters"], []any{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
	if got, want := typescriptSemantics["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("typescript_semantics[declaration_merge_group] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "declaration_merge"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleCallChainReturnsShortestPath(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNeo4j,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "shortestPath") {
					t.Fatalf("cypher = %q, want shortestPath query", cypher)
				}
				if strings.Contains(cypher, "CALLS_FUNCTION") {
					t.Fatalf("cypher = %q, want canonical CALLS edges only", cypher)
				}
				if !strings.Contains(cypher, "[:CALLS*1..6]") {
					t.Fatalf("cypher = %q, want bounded CALLS traversal", cypher)
				}
				if !strings.Contains(cypher, "RETURN [node IN nodes(path)") {
					t.Fatalf("cypher = %q, want projected node maps on the Neo4j path", cypher)
				}
				if !strings.Contains(cypher, "start.name = $start") {
					t.Fatalf("cypher = %q, want exact start-name predicate", cypher)
				}
				if !strings.Contains(cypher, "end.name = $end") {
					t.Fatalf("cypher = %q, want exact end-name predicate", cypher)
				}
				if got, want := params["start"], "wrapper"; got != want {
					t.Fatalf("params[start] = %#v, want %#v", got, want)
				}
				if got, want := params["end"], "helper"; got != want {
					t.Fatalf("params[end] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{"id": "fn-1", "name": "wrapper", "labels": []any{"Function"}},
							map[string]any{"id": "fn-2", "name": "delegate", "labels": []any{"Function"}},
							map[string]any{"id": "fn-3", "name": "helper", "labels": []any{"Function"}},
						},
						"depth": int64(2),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start":"wrapper","end":"helper","max_depth":6}`),
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
	chains, ok := resp["chains"].([]any)
	if !ok {
		t.Fatalf("resp[chains] type = %T, want []any", resp["chains"])
	}
	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(resp[chains]) = %d, want %d", got, want)
	}
}

func TestHandleCallChainUsesNornicDBBFSForNameAnchors(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "shortestPath") || strings.Contains(cypher, "CALLS*") {
					t.Fatalf("cypher = %q, must not use NornicDB shortestPath for call-chain", cypher)
				}
				if strings.Contains(cypher, "CALLS_FUNCTION") {
					t.Fatalf("cypher = %q, want canonical CALLS edges only", cypher)
				}
				if strings.Contains(cypher, "MATCH (e)<-[:CONTAINS]-(f:File)") {
					switch params["name"] {
					case "wrapper":
						return []map[string]any{{"id": "fn-1", "name": "wrapper", "labels": []any{"Function"}}}, nil
					case "helper":
						return []map[string]any{{"id": "fn-3", "name": "helper", "labels": []any{"Function"}}}, nil
					default:
						t.Fatalf("params[name] = %#v, want wrapper or helper", params["name"])
					}
				}
				if !strings.Contains(cypher, "MATCH (source:Function {uid: $source_id})-[:CALLS]->(target)") {
					t.Fatalf("cypher = %q, want one-hop CALLS traversal", cypher)
				}
				switch params["source_id"] {
				case "fn-1":
					return []map[string]any{{"id": "fn-2", "name": "delegate", "labels": []any{"Function"}}}, nil
				case "fn-2":
					return []map[string]any{{"id": "fn-3", "name": "helper", "labels": []any{"Function"}}}, nil
				default:
					return []map[string]any{}, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start":"wrapper","end":"helper","max_depth":6}`),
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
	chains, ok := resp["chains"].([]any)
	if !ok || len(chains) != 1 {
		t.Fatalf("resp[chains] = %#v, want one chain", resp["chains"])
	}
	chainResp, ok := chains[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[chains][0] type = %T, want map[string]any", chains[0])
	}
	chain, ok := chainResp["chain"].([]any)
	if !ok || len(chain) != 3 {
		t.Fatalf("resp[chains][0][chain] = %#v, want three nodes", chainResp["chain"])
	}
}

func TestHandleCallChainSupportsEntityIDAndRepoScopedLookup(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, graphEntityIDPredicate("start", "$start_entity_id")) {
					t.Fatalf("cypher = %q, want bridged start entity-id predicate", cypher)
				}
				if !strings.Contains(cypher, graphEntityIDPredicate("end", "$end_entity_id")) {
					t.Fatalf("cypher = %q, want bridged end entity-id predicate", cypher)
				}
				if !strings.Contains(cypher, "start.repo_id = $repo_id") ||
					!strings.Contains(cypher, "end.repo_id = $repo_id") {
					t.Fatalf("cypher = %q, want repo scoping for both endpoints", cypher)
				}
				if got, want := params["start_entity_id"], "fn-1"; got != want {
					t.Fatalf("params[start_entity_id] = %#v, want %#v", got, want)
				}
				if got, want := params["end_entity_id"], "fn-3"; got != want {
					t.Fatalf("params[end_entity_id] = %#v, want %#v", got, want)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if _, ok := params["start"]; ok {
					t.Fatalf("params[start] present = %#v, want omitted for entity-id lookup", params["start"])
				}
				if _, ok := params["end"]; ok {
					t.Fatalf("params[end] present = %#v, want omitted for entity-id lookup", params["end"])
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{"id": "fn-1", "name": "wrapper", "labels": []any{"Function"}},
							map[string]any{"id": "fn-2", "name": "delegate", "labels": []any{"Function"}},
							map[string]any{"id": "fn-3", "name": "helper", "labels": []any{"Function"}},
						},
						"depth": int64(2),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"fn-1","end_entity_id":"fn-3","repo_id":"repo-1","max_depth":4}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCallChainSupportsEntityIDAndRepoScopedLookupForNornicDB(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "shortestPath") || strings.Contains(cypher, "CALLS*") {
					t.Fatalf("cypher = %q, must not use NornicDB shortestPath for call-chain", cypher)
				}
				if strings.Contains(cypher, "MATCH (e") {
					if got, want := params["repo_id"], "repo-1"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					switch params["entity_id"] {
					case "fn-1":
						return []map[string]any{{"id": "fn-1", "name": "wrapper", "labels": []any{"Function"}}}, nil
					case "fn-3":
						return []map[string]any{{"id": "fn-3", "name": "helper", "labels": []any{"Function"}}}, nil
					default:
						t.Fatalf("params[entity_id] = %#v, want fn-1 or fn-3", params["entity_id"])
					}
				}
				if !strings.Contains(cypher, "MATCH (source:Function {uid: $source_id})-[:CALLS]->(target)") {
					t.Fatalf("cypher = %q, want one-hop CALLS traversal", cypher)
				}
				switch params["source_id"] {
				case "fn-1":
					return []map[string]any{{"id": "fn-2", "name": "delegate", "labels": []any{"Function"}}}, nil
				case "fn-2":
					return []map[string]any{{"id": "fn-3", "name": "helper", "labels": []any{"Function"}}}, nil
				default:
					return []map[string]any{}, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"fn-1","end_entity_id":"fn-3","repo_id":"repo-1","max_depth":4}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleRelationshipsReturnsTransitiveCallers(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNeo4j,
		Profile:      ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (e)<-[:CONTAINS]-(f:File)") {
					return []map[string]any{{
						"id":         "fn-helper",
						"name":       "helper",
						"labels":     []any{"Function"},
						"file_path":  "src/helper.go",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "go",
						"start_line": int64(20),
						"end_line":   int64(42),
					}}, nil
				}
				if !strings.Contains(cypher, "MATCH (e)") {
					t.Fatalf("cypher = %q, want explicit entity match", cypher)
				}
				if !strings.Contains(cypher, graphEntityIDPredicate("e", "$entity_id")) {
					t.Fatalf("cypher = %q, want bridged entity-id predicate", cypher)
				}
				if !strings.Contains(cypher, "MATCH path = (e)<-[:CALLS*1..7]-(source)") {
					t.Fatalf("cypher = %q, want transitive incoming CALLS traversal", cypher)
				}
				if !strings.Contains(cypher, "source.name as source_name") {
					t.Fatalf("cypher = %q, want source metadata projection", cypher)
				}
				if !strings.Contains(cypher, "length(path) as depth") {
					t.Fatalf("cypher = %q, want explicit depth projection", cypher)
				}
				if got, want := params["entity_id"], "fn-helper"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{{
					"id":          "fn-helper",
					"name":        "helper",
					"labels":      []any{"Function"},
					"file_path":   "src/helper.go",
					"repo_id":     "repo-1",
					"repo_name":   "payments",
					"language":    "go",
					"start_line":  int64(20),
					"end_line":    int64(42),
					"source_name": "delegate",
					"source_id":   "fn-delegate",
					"depth":       int64(1),
				}, {
					"id":          "fn-helper",
					"name":        "helper",
					"labels":      []any{"Function"},
					"file_path":   "src/helper.go",
					"repo_id":     "repo-1",
					"repo_name":   "payments",
					"language":    "go",
					"start_line":  int64(20),
					"end_line":    int64(42),
					"source_name": "wrapper",
					"source_id":   "fn-wrapper",
					"depth":       int64(2),
				}}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"helper","direction":"incoming","relationship_type":"CALLS","transitive":true,"max_depth":7}`),
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
	incoming, ok := resp["incoming"].([]any)
	if !ok || len(incoming) != 2 {
		t.Fatalf("resp[incoming] = %#v, want two transitive callers", resp["incoming"])
	}
	first, ok := incoming[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[incoming][0] type = %T, want map[string]any", incoming[0])
	}
	if got, want := first["depth"], float64(1); got != want {
		t.Fatalf("resp[incoming][0][depth] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipsReturnsTransitiveCallersForNornicDB(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Profile:      ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (e)<-[:CONTAINS]-(f:File)") {
					return []map[string]any{{
						"id":         "fn-helper",
						"name":       "helper",
						"labels":     []any{"Function"},
						"file_path":  "src/helper.go",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "go",
						"start_line": int64(20),
						"end_line":   int64(42),
					}}, nil
				}
				if strings.Contains(cypher, "CALLS*") || strings.Contains(cypher, "length(path)") {
					t.Fatalf("cypher = %q, must not depend on NornicDB variable-path length", cypher)
				}
				if !strings.Contains(cypher, "MATCH (source)-[:CALLS]->(target)") {
					t.Fatalf("cypher = %q, want one-hop CALLS traversal for NornicDB", cypher)
				}
				switch params["entity_id"] {
				case "fn-helper":
					return []map[string]any{{
						"source_name": "delegate",
						"source_id":   "fn-delegate",
						"target_name": "helper",
						"target_id":   "fn-helper",
					}}, nil
				case "fn-delegate":
					return []map[string]any{{
						"source_name": "wrapper",
						"source_id":   "fn-wrapper",
						"target_name": "delegate",
						"target_id":   "fn-delegate",
					}}, nil
				default:
					return []map[string]any{}, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"helper","direction":"incoming","relationship_type":"CALLS","transitive":true}`),
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
	incoming, ok := resp["incoming"].([]any)
	if !ok || len(incoming) != 2 {
		t.Fatalf("resp[incoming] = %#v, want two transitive callers", resp["incoming"])
	}
}

func TestHandleCallChainSupportsRustImplContextQualifiedLookup(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "shortestPath") {
					t.Fatalf("cypher = %q, want shortestPath query", cypher)
				}
				if strings.Contains(cypher, "CALLS_FUNCTION") {
					t.Fatalf("cypher = %q, want canonical CALLS edges only", cypher)
				}
				if !strings.Contains(cypher, "[:CALLS*1..3]") {
					t.Fatalf("cypher = %q, want bounded CALLS traversal", cypher)
				}
				if !strings.Contains(cypher, graphEntityIDPredicate("start", "$start_entity_id")) {
					t.Fatalf("cypher = %q, want bridged start entity-id predicate", cypher)
				}
				if !strings.Contains(cypher, graphEntityIDPredicate("end", "$end_entity_id")) {
					t.Fatalf("cypher = %q, want bridged end entity-id predicate", cypher)
				}
				if got, want := params["start_entity_id"], "fn-new"; got != want {
					t.Fatalf("params[start_entity_id] = %#v, want %#v", got, want)
				}
				if got, want := params["end_entity_id"], "fn-x"; got != want {
					t.Fatalf("params[end_entity_id] = %#v, want %#v", got, want)
				}
				if got, want := params["repo_id"], "repo-rust"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{"id": "fn-new", "name": "new", "labels": []any{"Function"}},
							map[string]any{"id": "fn-x", "name": "x", "labels": []any{"Function"}},
						},
						"depth": int64(1),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"fn-new","end_entity_id":"fn-x","repo_id":"repo-rust","max_depth":3}`),
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
	chains, ok := resp["chains"].([]any)
	if !ok {
		t.Fatalf("resp[chains] type = %T, want []any", resp["chains"])
	}
	if got, want := len(chains), 1; got != want {
		t.Fatalf("len(resp[chains]) = %d, want %d", got, want)
	}
	chain, ok := chains[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[chains][0] type = %T, want map[string]any", chains[0])
	}
	nodes, ok := chain["chain"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("resp[chains][0][chain] = %#v, want two Rust impl-method nodes", chain["chain"])
	}
}
