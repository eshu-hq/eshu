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

type relationshipStoryContentStore struct {
	fakePortContentStore
	matches []EntityContent
	entity  *EntityContent
}

func (s relationshipStoryContentStore) GetEntityContent(context.Context, string) (*EntityContent, error) {
	if s.entity != nil {
		entity := *s.entity
		return &entity, nil
	}
	return nil, nil
}

func (s relationshipStoryContentStore) SearchEntitiesByName(
	context.Context,
	string,
	string,
	string,
	int,
) ([]EntityContent, error) {
	return append([]EntityContent(nil), s.matches...), nil
}

func (s relationshipStoryContentStore) SearchEntitiesByNameAnyRepo(
	context.Context,
	string,
	string,
	int,
) ([]EntityContent, error) {
	return append([]EntityContent(nil), s.matches...), nil
}

func (s relationshipStoryContentStore) SearchEntitiesByLanguageAndType(
	context.Context,
	string,
	string,
	string,
	string,
	int,
) ([]EntityContent, error) {
	return append([]EntityContent(nil), s.matches...), nil
}

func TestHandleRelationshipStoryReturnsAmbiguousCandidatesWithoutGuessing(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: relationshipStoryContentStore{
			matches: []EntityContent{
				{
					EntityID:     "function-a",
					RepoID:       "repo-1",
					RelativePath: "payments/charge.go",
					EntityType:   "Function",
					EntityName:   "process_payment",
					Language:     "go",
					StartLine:    12,
					EndLine:      20,
				},
				{
					EntityID:     "function-b",
					RepoID:       "repo-1",
					RelativePath: "billing/charge.go",
					EntityType:   "Function",
					EntityName:   "process_payment",
					Language:     "go",
					StartLine:    44,
					EndLine:      59,
				},
			},
		},
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("relationship story must not query graph when target resolution is ambiguous")
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"target":"process_payment","repo_id":"repo-1","relationship_type":"CALLS","direction":"incoming","limit":1}`),
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
	resolution, ok := resp["target_resolution"].(map[string]any)
	if !ok {
		t.Fatalf("target_resolution type = %T, want map[string]any", resp["target_resolution"])
	}
	if got, want := resolution["status"], "ambiguous"; got != want {
		t.Fatalf("target_resolution.status = %#v, want %#v", got, want)
	}
	candidates, ok := resolution["candidates"].([]any)
	if !ok {
		t.Fatalf("target_resolution.candidates type = %T, want []any", resolution["candidates"])
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := resolution["truncated"], true; got != want {
		t.Fatalf("target_resolution.truncated = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryUsesBoundedGraphQuery(t *testing.T) {
	t.Parallel()

	var calls int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				calls++
				if !strings.Contains(cypher, "ORDER BY") {
					t.Fatalf("cypher = %q, want deterministic ordering", cypher)
				}
				if !strings.Contains(cypher, "SKIP $offset") || !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded pagination", cypher)
				}
				if got, want := params["entity_id"], "function-target"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				if got, want := params["offset"], 0; got != want {
					t.Fatalf("params[offset] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"direction":   "incoming",
						"type":        "CALLS",
						"source_id":   "function-caller",
						"source_name": "caller",
						"target_id":   "function-target",
						"target_name": "process_payment",
					},
					{
						"direction":   "incoming",
						"type":        "CALLS",
						"source_id":   "function-other",
						"source_name": "other",
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
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming","limit":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := calls, 1; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage type = %T, want map[string]any", resp["coverage"])
	}
	if got, want := coverage["truncated"], true; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryBothDirectionsExposeDirectionCoverage(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "'incoming' as direction") {
					return []map[string]any{
						{"direction": "incoming", "type": "CALLS", "source_id": "caller-a", "source_name": "callerA"},
						{"direction": "incoming", "type": "CALLS", "source_id": "caller-b", "source_name": "callerB"},
						{"direction": "incoming", "type": "CALLS", "source_id": "caller-c", "source_name": "callerC"},
					}, nil
				}
				return []map[string]any{
					{"direction": "outgoing", "type": "CALLS", "target_id": "callee-a", "target_name": "calleeA"},
					{"direction": "outgoing", "type": "CALLS", "target_id": "callee-b", "target_name": "calleeB"},
					{"direction": "outgoing", "type": "CALLS", "target_id": "callee-c", "target_name": "calleeC"},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","limit":2}`),
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
	first := relationships[0].(map[string]any)
	second := relationships[1].(map[string]any)
	if got, want := first["direction"], "incoming"; got != want {
		t.Fatalf("relationships[0].direction = %#v, want %#v", got, want)
	}
	if got, want := second["direction"], "outgoing"; got != want {
		t.Fatalf("relationships[1].direction = %#v, want %#v", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	returned := coverage["returned_by_direction"].(map[string]any)
	if got, want := returned["outgoing"], float64(1); got != want {
		t.Fatalf("coverage.returned_by_direction[outgoing] = %#v, want %#v", got, want)
	}
	truncated := coverage["truncated_by_direction"].(map[string]any)
	if got, want := truncated["incoming"], true; got != want {
		t.Fatalf("coverage.truncated_by_direction[incoming] = %#v, want %#v", got, want)
	}
	if got, want := truncated["outgoing"], true; got != want {
		t.Fatalf("coverage.truncated_by_direction[outgoing] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryReportsOneHopDepthForDirectRead(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-target","relationship_type":"CALLS","direction":"incoming"}`),
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
	scope := resp["scope"].(map[string]any)
	if got, want := scope["max_depth"], float64(1); got != want {
		t.Fatalf("scope.max_depth = %#v, want %#v", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	if got, want := coverage["max_depth"], float64(1); got != want {
		t.Fatalf("coverage.max_depth = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryTraversesTransitiveCallsWithDepthLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				switch params["entity_id"] {
				case "function-root":
					return []map[string]any{
						{
							"direction":   "outgoing",
							"type":        "CALLS",
							"source_id":   "function-root",
							"source_name": "root",
							"target_id":   "function-mid",
							"target_name": "mid",
						},
					}, nil
				case "function-mid":
					return []map[string]any{
						{
							"direction":   "outgoing",
							"type":        "CALLS",
							"source_id":   "function-mid",
							"source_name": "mid",
							"target_id":   "function-leaf",
							"target_name": "leaf",
						},
					}, nil
				default:
					t.Fatalf("unexpected graph entity_id %#v", params["entity_id"])
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-root","relationship_type":"CALLS","direction":"outgoing","include_transitive":true,"max_depth":2,"limit":5}`),
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
	second, ok := relationships[1].(map[string]any)
	if !ok {
		t.Fatalf("relationships[1] type = %T, want map[string]any", relationships[1])
	}
	if got, want := second["depth"], float64(2); got != want {
		t.Fatalf("relationships[1].depth = %#v, want %#v", got, want)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage type = %T, want map[string]any", resp["coverage"])
	}
	if got, want := coverage["query_shape"], "entity_anchor_bounded_bfs"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryGuidesTransitiveDirection(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-root","include_transitive":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "set direction to incoming or outgoing when include_transitive is true") {
		t.Fatalf("body = %q, want explicit direction guidance", w.Body.String())
	}
}

func TestHandleRelationshipStoryRejectsTransitiveOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"function-root","direction":"outgoing","include_transitive":true,"offset":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "include_transitive requires offset 0") {
		t.Fatalf("body = %q, want transitive offset error", w.Body.String())
	}
}

func TestNornicDBRelationshipStoryCypherUsesAnchoredPatternAndPagination(t *testing.T) {
	t.Parallel()

	cypher, params := nornicDBRelationshipStoryGraphCypher(
		relationshipStoryRequest{
			EntityID:         "function-root",
			RelationshipType: "CALLS",
			Limit:            25,
		},
		"function-root",
		"Function",
		"uid",
		"outgoing",
	)

	for _, fragment := range []string{
		"MATCH (anchor:Function {uid: $entity_id})-[rel:CALLS]->(target)",
		"ORDER BY target.name, target_id",
		"SKIP $offset",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", cypher, fragment)
		}
	}
	if strings.Contains(cypher, graphEntityIDPredicate("anchor", "$entity_id")) {
		t.Fatalf("cypher = %q, must not use broad id-or-uid predicate", cypher)
	}
	if got, want := params["limit"], 26; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
}
