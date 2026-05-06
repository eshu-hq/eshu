package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesRustImplScopedCallsUsingImplContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "main.rs")
	calleePath := filepath.Join(repoRoot, "impl_blocks.rs")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-rust",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "main.rs",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "main",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:rust-main",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "new",
							"full_name":   "Point::new",
							"line_number": 3,
							"lang":        "rust",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"relative_path": "impl_blocks.rs",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":         "new",
							"impl_context": "Point",
							"line_number":  2,
							"end_line":     4,
							"uid":          "content-entity:rust-point-new",
						},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractCodeCallRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-rust" {
		t.Fatalf("repoIDs = %v, want [repo-rust]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["caller_entity_id"], "content-entity:rust-main"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:rust-point-new"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["callee_file"], "impl_blocks.rs"; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}
}
