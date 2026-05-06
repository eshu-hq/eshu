package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossFileRepoUniqueBareCalls(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-platform",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-platform",
				"relative_path": "service.go",
				"parsed_file_data": map[string]any{
					"path": "service.go",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    6,
							"uid":         "content-entity:go-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "helper",
							"full_name":   "helper",
							"line_number": 4,
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-platform",
				"relative_path": "helper.go",
				"parsed_file_data": map[string]any{
					"path": "helper.go",
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:go-helper",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:go-run"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:go-helper"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "helper.go"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsSkipsAmbiguousCrossFileRepoBareCalls(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-platform",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-platform",
				"relative_path": "service.go",
				"parsed_file_data": map[string]any{
					"path": "service.go",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    6,
							"uid":         "content-entity:go-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "helper",
							"full_name":   "helper",
							"line_number": 4,
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-platform",
				"relative_path": "helper_a.go",
				"parsed_file_data": map[string]any{
					"path": "helper_a.go",
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:go-helper-a",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-platform",
				"relative_path": "helper_b.go",
				"parsed_file_data": map[string]any{
					"path": "helper_b.go",
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:go-helper-b",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for ambiguous cross-file repo bare call", len(rows))
	}
}
