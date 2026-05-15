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

func TestHandleRelationshipStoryReturnsClassHierarchyPacket(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["entity_id"], "class-a"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded limit", cypher)
				}
				switch {
				case strings.Contains(cypher, "[:INHERITS*1..4]"):
					return []map[string]any{
						{"direction": "outgoing", "target_id": "base", "target_name": "Base", "depth": 1},
						{"direction": "outgoing", "target_id": "root", "target_name": "Root", "depth": 2},
					}, nil
				case strings.Contains(cypher, "WHERE (target.id = $entity_id"):
					return []map[string]any{
						{
							"direction":   "incoming",
							"type":        "INHERITS",
							"source_id":   "child",
							"source_name": "Child",
							"target_id":   "class-a",
							"target_name": "A",
						},
					}, nil
				case strings.Contains(cypher, "WHERE (source.id = $entity_id"):
					return []map[string]any{
						{
							"direction":   "outgoing",
							"type":        "INHERITS",
							"source_id":   "class-a",
							"source_name": "A",
							"target_id":   "base",
							"target_name": "Base",
						},
					}, nil
				case strings.Contains(cypher, "-[:CONTAINS]->(method:Function)"):
					return []map[string]any{
						{"method_id": "method-run", "method_name": "run", "start_line": 12, "end_line": 19},
						{"method_id": "method-stop", "method_name": "stop", "start_line": 21, "end_line": 23},
					}, nil
				default:
					t.Fatalf("unexpected cypher = %q", cypher)
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
		bytes.NewBufferString(`{"query_type":"class_hierarchy","entity_id":"class-a","relationship_type":"INHERITS","max_depth":4,"limit":2}`),
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
	hierarchy, ok := resp["class_hierarchy"].(map[string]any)
	if !ok {
		t.Fatalf("class_hierarchy type = %T, want map[string]any", resp["class_hierarchy"])
	}
	methods, ok := hierarchy["methods"].([]any)
	if !ok || len(methods) != 2 {
		t.Fatalf("class_hierarchy.methods = %#v, want 2 methods", hierarchy["methods"])
	}
	parents, ok := hierarchy["parents"].([]any)
	if !ok || len(parents) != 1 {
		t.Fatalf("class_hierarchy.parents = %#v, want 1 parent", hierarchy["parents"])
	}
	children, ok := hierarchy["children"].([]any)
	if !ok || len(children) != 1 {
		t.Fatalf("class_hierarchy.children = %#v, want 1 child", hierarchy["children"])
	}
	depth := hierarchy["depth_summary"].(map[string]any)
	if got, want := depth["max_parent_depth"], float64(2); got != want {
		t.Fatalf("depth_summary.max_parent_depth = %#v, want %#v", got, want)
	}
	coverage := resp["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "entity_anchor_class_hierarchy_story"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryListsOverridesWithoutTarget(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})") {
					t.Fatalf("cypher = %q, want repo-anchored override query", cypher)
				}
				if !strings.Contains(cypher, "-[rel:OVERRIDES]->") {
					t.Fatalf("cypher = %q, want OVERRIDES edge query", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"direction":   "outgoing",
						"type":        "OVERRIDES",
						"source_id":   "child-run",
						"source_name": "run",
						"target_id":   "base-run",
						"target_name": "run",
					},
					{
						"direction":   "outgoing",
						"type":        "OVERRIDES",
						"source_id":   "child-stop",
						"source_name": "stop",
						"target_id":   "base-stop",
						"target_name": "stop",
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
		bytes.NewBufferString(`{"query_type":"overrides","repo_id":"repo-1","limit":1}`),
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
	story, ok := resp["override_story"].(map[string]any)
	if !ok {
		t.Fatalf("override_story type = %T, want map[string]any", resp["override_story"])
	}
	overrides, ok := story["overrides"].([]any)
	if !ok || len(overrides) != 1 {
		t.Fatalf("override_story.overrides = %#v, want 1 paged override", story["overrides"])
	}
	summary := resp["summary"].(map[string]any)
	if got, want := summary["truncated"], true; got != want {
		t.Fatalf("summary.truncated = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipStoryRequiresRepoForTargetlessOverrides(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("targetless overrides without repo_id must fail before graph query")
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"query_type":"overrides","limit":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo_id is required for repo-scoped overrides") {
		t.Fatalf("body = %q, want repo_id guidance", w.Body.String())
	}
}

func TestNornicDBRelationshipStoryClassMethodsCypherUsesAnchoredClassPattern(t *testing.T) {
	t.Parallel()

	cypher, params := nornicDBRelationshipStoryClassMethodsCypher(
		relationshipStoryRequest{Limit: 10, Offset: 5},
		"class-a",
		"uid",
	)
	for _, fragment := range []string{
		"MATCH (class:Class {uid: $entity_id})-[:CONTAINS]->(method:Function)",
		"ORDER BY method.name, method_id",
		"SKIP $offset",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", cypher, fragment)
		}
	}
	if got, want := params["entity_id"], "class-a"; got != want {
		t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := params["limit"], 11; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
}

func TestNornicDBRelationshipStoryInheritanceDepthCypherBoundsTraversal(t *testing.T) {
	t.Parallel()

	cypher, params := nornicDBRelationshipStoryInheritanceDepthCypher(
		relationshipStoryRequest{MaxDepth: 4, Limit: 25},
		"class-a",
		"outgoing",
		"uid",
	)
	for _, fragment := range []string{
		"MATCH path = (anchor:Class {uid: $entity_id})-[:INHERITS*1..4]->(target:Class)",
		"ORDER BY depth, target.name, target_id",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", cypher, fragment)
		}
	}
	if got, want := params["limit"], 26; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
}
