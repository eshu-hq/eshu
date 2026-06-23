package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeCallResolutionMethodKotlinImportBinding proves a Kotlin receiver call
// whose inferred type resolves through an explicit import binds to the imported
// file's declaration rather than a same-named decoy elsewhere in the repository.
func TestCodeCallResolutionMethodKotlinImportBinding(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-kotlin",
			"imports_map": map[string][]string{
				"Service": {
					"src/main/kotlin/com/example/lib/Service.kt",
					"src/main/kotlin/com/other/Service.kt",
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/example/Caller.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/example/Caller.kt",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{
						"name":        "com.example.lib.Service",
						"alias":       "Service",
						"source":      "com.example.lib.Service",
						"import_type": "import",
						"lang":        "kotlin",
					},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "query",
						"full_name":         "service.query",
						"inferred_obj_type": "Service",
						"argument_types":    []any{"Task"},
						"argument_count":    1,
						"line_number":       5,
						"lang":              "kotlin",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/com/example/lib/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/com/example/lib/Service.kt",
				"functions": []any{
					map[string]any{
						"name":            "query",
						"class_context":   "Service",
						"parameter_types": []any{"Task"},
						"line_number":     2,
						"end_line":        4,
						"uid":             "uid:query",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/com/other/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/com/other/Service.kt",
				"functions": []any{
					map[string]any{
						"name":            "query",
						"class_context":   "Service",
						"parameter_types": []any{"Task"},
						"line_number":     2,
						"end_line":        4,
						"uid":             "uid:query-decoy",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:query"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:query-decoy")
}

// TestCodeCallResolutionMethodKotlinAliasedImportBinding proves Kotlin's aliased
// `import a.b.Service as Svc` (emitted with import_type "alias") binds the
// receiver call to the imported declaration even though the prescan import map
// and the callee declaration are keyed by the declared type (`Service`), not the
// alias (`Svc`). The facts mirror production: the parser emits the alias as the
// receiver's inferred_obj_type while the prescan keys imports_map by the
// declared class name and the callee's class_context is the declared class name.
func TestCodeCallResolutionMethodKotlinAliasedImportBinding(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-kotlin",
			"imports_map": map[string][]string{
				"Service": {"src/main/kotlin/com/example/lib/Service.kt"},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/example/Caller.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/example/Caller.kt",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{
						"name":        "com.example.lib.Service",
						"alias":       "Svc",
						"source":      "com.example.lib.Service",
						"import_type": "alias",
						"lang":        "kotlin",
					},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "query",
						"full_name":         "svc.query",
						"inferred_obj_type": "Svc",
						"line_number":       5,
						"lang":              "kotlin",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/com/example/lib/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/com/example/lib/Service.kt",
				"functions": []any{
					map[string]any{
						"name":          "query",
						"class_context": "Service",
						"line_number":   2,
						"end_line":      4,
						"uid":           "uid:query",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:query"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

// TestCodeCallResolutionMethodKotlinImportBindingNonMatchingFilename proves a
// Kotlin receiver call binds through its import even when the imported class is
// declared in a differently named file (`class Service` in `Domain.kt`), which
// Kotlin allows. The prescan keys imports_map by the declared type and points it
// at the real file, so the resolver must not require the `.kt` filename to equal
// the type name. The decoy in another package must stay unbound so the package
// directory still disambiguates.
func TestCodeCallResolutionMethodKotlinImportBindingNonMatchingFilename(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-kotlin",
			"imports_map": map[string][]string{
				"Service": {
					"src/main/kotlin/com/example/lib/Domain.kt",
					"src/main/kotlin/com/other/Service.kt",
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/example/Caller.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/example/Caller.kt",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{
						"name":        "com.example.lib.Service",
						"alias":       "Service",
						"source":      "com.example.lib.Service",
						"import_type": "import",
						"lang":        "kotlin",
					},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "query",
						"full_name":         "service.query",
						"inferred_obj_type": "Service",
						"line_number":       5,
						"lang":              "kotlin",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/com/example/lib/Domain.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/com/example/lib/Domain.kt",
				"functions": []any{
					map[string]any{
						"name":          "query",
						"class_context": "Service",
						"line_number":   2,
						"end_line":      4,
						"uid":           "uid:query",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/com/other/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/com/other/Service.kt",
				"functions": []any{
					map[string]any{
						"name":          "query",
						"class_context": "Service",
						"line_number":   2,
						"end_line":      4,
						"uid":           "uid:query-decoy",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:query"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:query-decoy")
}

// TestCodeCallKotlinImportedReceiverBlocksAmbiguousRepoFallback proves that when
// an imported receiver type binds to two same-named declarations, the resolver
// refuses to invent a repo-unique fallback edge.
func TestCodeCallKotlinImportedReceiverBlocksAmbiguousRepoFallback(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-kotlin",
			"imports_map": map[string][]string{
				"Service": {
					"module-a/src/main/kotlin/com/example/lib/Service.kt",
					"module-b/src/main/kotlin/com/example/lib/Service.kt",
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "src/main/kotlin/example/Caller.kt",
			"parsed_file_data": map[string]any{
				"path": "src/main/kotlin/example/Caller.kt",
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{
						"name":        "com.example.lib.Service",
						"alias":       "Service",
						"source":      "com.example.lib.Service",
						"import_type": "import",
						"lang":        "kotlin",
					},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "query",
						"full_name":         "service.query",
						"inferred_obj_type": "Service",
						"line_number":       5,
						"lang":              "kotlin",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "module-a/src/main/kotlin/com/example/lib/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "module-a/src/main/kotlin/com/example/lib/Service.kt",
				"functions": []any{
					map[string]any{"name": "query", "class_context": "Service", "line_number": 2, "end_line": 4, "uid": "uid:query-a"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-kotlin",
			"relative_path": "module-b/src/main/kotlin/com/example/lib/Service.kt",
			"parsed_file_data": map[string]any{
				"path": "module-b/src/main/kotlin/com/example/lib/Service.kt",
				"functions": []any{
					map[string]any{"name": "query", "class_context": "Service", "line_number": 2, "end_line": 4, "uid": "uid:query-b"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:query-a")
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:query-b")
}
