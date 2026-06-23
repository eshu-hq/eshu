package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeCallResolutionMethodSwiftReceiverTypeInferred proves a Swift call on a
// receiver whose inferred type (inferred_obj_type) names a class binds to that
// class's uniquely named method, recording type-inference provenance, rather
// than a same-named method on an unrelated type.
func TestCodeCallResolutionMethodSwiftReceiverTypeInferred(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/App/Caller.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/App/Caller.swift",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 1, "end_line": 5, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "info",
						"full_name":         "logger.info",
						"inferred_obj_type": "Logger",
						"line_number":       3,
						"lang":              "swift",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/Lib/Logger.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/Lib/Logger.swift",
				"classes": []any{
					map[string]any{"name": "Logger", "line_number": 1, "end_line": 6, "uid": "uid:logger-class", "lang": "swift"},
				},
				"functions": []any{
					map[string]any{"name": "info", "class_context": "Logger", "line_number": 2, "end_line": 4, "uid": "uid:logger-info", "lang": "swift"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/Other/Printer.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/Other/Printer.swift",
				"classes": []any{
					map[string]any{"name": "Printer", "line_number": 1, "end_line": 6, "uid": "uid:printer-class", "lang": "swift"},
				},
				"functions": []any{
					map[string]any{"name": "info", "class_context": "Printer", "line_number": 2, "end_line": 4, "uid": "uid:printer-info", "lang": "swift"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:logger-info"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:printer-info")
}

// TestCodeCallSwiftAmbiguousReceiverMethodUnresolved proves Swift receiver-type
// resolution refuses to bind when the same class name owns two declarations of
// the called method, so an ambiguous guess never becomes graph truth.
func TestCodeCallSwiftAmbiguousReceiverMethodUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/App/Caller.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/App/Caller.swift",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 1, "end_line": 5, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "info",
						"full_name":         "logger.info",
						"inferred_obj_type": "Logger",
						"line_number":       3,
						"lang":              "swift",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/LibA/Logger.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/LibA/Logger.swift",
				"functions": []any{
					map[string]any{"name": "info", "class_context": "Logger", "line_number": 2, "end_line": 4, "uid": "uid:logger-info-a", "lang": "swift"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-swift",
			"relative_path": "Sources/LibB/Logger.swift",
			"parsed_file_data": map[string]any{
				"path": "Sources/LibB/Logger.swift",
				"functions": []any{
					map[string]any{"name": "info", "class_context": "Logger", "line_number": 2, "end_line": 4, "uid": "uid:logger-info-b", "lang": "swift"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:logger-info-a")
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:logger-info-b")
}
