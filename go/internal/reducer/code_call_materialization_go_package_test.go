package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesGoBareCallWithinDirectoryBeforeRepoAmbiguity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "go/cmd/api/main.go",
				"parsed_file_data": map[string]any{
					"path": "go/cmd/api/main.go",
					"functions": []any{
						map[string]any{"name": "main", "line_number": 20, "end_line": 87, "uid": "content-entity:api-main"},
					},
					"function_calls": []any{
						map[string]any{"name": "wireAPI", "full_name": "wireAPI", "line_number": 50, "lang": "go"},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "go/cmd/api/wiring.go",
				"parsed_file_data": map[string]any{
					"path": "go/cmd/api/wiring.go",
					"functions": []any{
						map[string]any{"name": "wireAPI", "line_number": 27, "end_line": 116, "uid": "content-entity:api-wire"},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "go/cmd/mcp-server/wiring.go",
				"parsed_file_data": map[string]any{
					"path": "go/cmd/mcp-server/wiring.go",
					"functions": []any{
						map[string]any{"name": "wireAPI", "line_number": 27, "end_line": 116, "uid": "content-entity:mcp-wire"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:api-main"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:api-wire"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "go/cmd/api/wiring.go"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsMaterializesGoCompositeLiteralTypeReferences(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-go",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "go/cmd/bootstrap-index/wiring.go",
				"parsed_file_data": map[string]any{
					"path": "go/cmd/bootstrap-index/wiring.go",
					"functions": []any{
						map[string]any{"name": "buildCollector", "line_number": 20, "end_line": 87, "uid": "content-entity:build-collector"},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "collectorDeps",
							"full_name":   "collectorDeps",
							"line_number": 73,
							"lang":        "go",
							"call_kind":   "go.composite_literal_type_reference",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-go",
				"relative_path": "go/cmd/bootstrap-index/main.go",
				"parsed_file_data": map[string]any{
					"path": "go/cmd/bootstrap-index/main.go",
					"structs": []any{
						map[string]any{"name": "collectorDeps", "line_number": 50, "end_line": 53, "uid": "content-entity:collector-deps"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:build-collector"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:collector-deps"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "REFERENCES"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
}
