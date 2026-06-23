package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeCallResolutionMethodJavaScriptReceiverTypeInferred proves a JavaScript
// call on a receiver whose inferred type names a class binds to that class's
// uniquely named method with type-inference provenance, not a same-named method
// on an unrelated class.
func TestCodeCallResolutionMethodJavaScriptReceiverTypeInferred(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/caller.js",
			"parsed_file_data": map[string]any{
				"path": "src/caller.js",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 1, "end_line": 5, "uid": "uid:caller", "lang": "javascript"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "invoke",
						"full_name":         "worker.invoke",
						"inferred_obj_type": "Worker",
						"line_number":       3,
						"lang":              "javascript",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/worker.js",
			"parsed_file_data": map[string]any{
				"path": "src/worker.js",
				"classes": []any{
					map[string]any{"name": "Worker", "line_number": 1, "end_line": 6, "uid": "uid:worker-class", "lang": "javascript"},
				},
				"functions": []any{
					map[string]any{"name": "invoke", "class_context": "Worker", "line_number": 2, "end_line": 4, "uid": "uid:worker-invoke", "lang": "javascript"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/sink.js",
			"parsed_file_data": map[string]any{
				"path": "src/sink.js",
				"classes": []any{
					map[string]any{"name": "Sink", "line_number": 1, "end_line": 6, "uid": "uid:sink-class", "lang": "javascript"},
				},
				"functions": []any{
					map[string]any{"name": "invoke", "class_context": "Sink", "line_number": 2, "end_line": 4, "uid": "uid:sink-invoke", "lang": "javascript"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:worker-invoke"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:sink-invoke")
}

// TestCodeCallResolutionMethodJsxReceiverTypeInferred proves the JSX dialect
// shares the JavaScript receiver-type resolver.
func TestCodeCallResolutionMethodJsxReceiverTypeInferred(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-jsx",
			"relative_path": "src/caller.jsx",
			"parsed_file_data": map[string]any{
				"path": "src/caller.jsx",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 1, "end_line": 5, "uid": "uid:caller", "lang": "jsx"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "invoke",
						"full_name":         "worker.invoke",
						"inferred_obj_type": "Worker",
						"line_number":       3,
						"lang":              "jsx",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-jsx",
			"relative_path": "src/worker.jsx",
			"parsed_file_data": map[string]any{
				"path": "src/worker.jsx",
				"functions": []any{
					map[string]any{"name": "invoke", "class_context": "Worker", "line_number": 2, "end_line": 4, "uid": "uid:worker-invoke", "lang": "jsx"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:worker-invoke"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
}

// TestCodeCallJavaScriptAmbiguousReceiverMethodUnresolved proves JavaScript
// receiver-type resolution refuses to bind when one class name owns two
// declarations of the called method, so an ambiguous guess never materializes.
func TestCodeCallJavaScriptAmbiguousReceiverMethodUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/caller.js",
			"parsed_file_data": map[string]any{
				"path": "src/caller.js",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 1, "end_line": 5, "uid": "uid:caller", "lang": "javascript"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "invoke",
						"full_name":         "worker.invoke",
						"inferred_obj_type": "Worker",
						"line_number":       3,
						"lang":              "javascript",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/a/worker.js",
			"parsed_file_data": map[string]any{
				"path": "src/a/worker.js",
				"functions": []any{
					map[string]any{"name": "invoke", "class_context": "Worker", "line_number": 2, "end_line": 4, "uid": "uid:worker-invoke-a", "lang": "javascript"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-js",
			"relative_path": "src/b/worker.js",
			"parsed_file_data": map[string]any{
				"path": "src/b/worker.js",
				"functions": []any{
					map[string]any{"name": "invoke", "class_context": "Worker", "line_number": 2, "end_line": 4, "uid": "uid:worker-invoke-b", "lang": "javascript"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:worker-invoke-a")
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:worker-invoke-b")
}
