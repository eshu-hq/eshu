package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallResolutionMethodDartImportBinding(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/service.dart"
	calleePath := "/repo/lib/src/helper.dart"
	decoyPath := "/repo/lib/src/other_helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
			"imports_map": map[string][]string{
				"helper": {calleePath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 6, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "src/helper.dart", "lang": "dart"},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "line_number": 5, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/helper.dart",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "helper", "line_number": 2, "end_line": 3, "uid": "uid:helper"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/other_helper.dart",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "helper", "line_number": 2, "end_line": 3, "uid": "uid:helper-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:helper"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper-decoy")
}

func TestCodeCallDartUnresolvedImportBlocksRepoUniqueFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/service.dart"
	decoyPath := "/repo/lib/src/local_helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-dart",
			"imports_map": map[string][]string{},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 6, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "src/helper.dart", "lang": "dart"},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "line_number": 5, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/local_helper.dart",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "helper", "line_number": 2, "end_line": 3, "uid": "uid:helper-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper-decoy")
}

func TestCodeCallDartClassImportBlocksCaseMismatchedRepoFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/service.dart"
	decoyPath := "/repo/lib/src/local_helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-dart",
			"imports_map": map[string][]string{},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"classes": []any{
					map[string]any{"name": "Runner", "line_number": 3, "end_line": 5, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "src/helper.dart", "lang": "dart"},
				},
				"function_calls": []any{
					map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/local_helper.dart",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"classes": []any{
					map[string]any{"name": "Helper", "line_number": 2, "end_line": 3, "uid": "uid:helper-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper-decoy")
}

func TestCodeCallDartExportDoesNotResolveAsImport(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/barrel.dart"
	calleePath := "/repo/lib/src/helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
			"imports_map": map[string][]string{
				"Helper": {calleePath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/barrel.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"classes": []any{
					map[string]any{"name": "Barrel", "line_number": 3, "end_line": 5, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "src/helper.dart", "lang": "dart", "import_type": "export"},
				},
				"function_calls": []any{
					map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/helper.dart",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"classes": []any{
					map[string]any{"name": "Helper", "line_number": 2, "end_line": 3, "uid": "uid:helper"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper")
}

func TestCodeCallDartPackageImportUsesCallerPackageRoot(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/packages/app/lib/service.dart"
	calleePath := "/repo/packages/app/lib/src/helper.dart"
	decoyPath := "/repo/lib/src/helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
			"imports_map": map[string][]string{
				"Helper": {calleePath, decoyPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "packages/app/lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"classes": []any{
					map[string]any{"name": "Runner", "line_number": 3, "end_line": 5, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "package:app/src/helper.dart", "lang": "dart", "import_type": "import"},
				},
				"function_calls": []any{
					map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "packages/app/lib/src/helper.dart",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"classes": []any{
					map[string]any{"name": "Helper", "line_number": 2, "end_line": 3, "uid": "uid:helper"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/helper.dart",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"classes": []any{
					map[string]any{"name": "Helper", "line_number": 2, "end_line": 3, "uid": "uid:helper-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:helper"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper-decoy")
}

func TestCodeCallDartExternalPackageImportDoesNotBindLocalFile(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/packages/app/lib/service.dart"
	decoyPath := "/repo/lib/src/helper.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
			"imports_map": map[string][]string{
				"Helper": {decoyPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "packages/app/lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"classes": []any{
					map[string]any{"name": "Runner", "line_number": 3, "end_line": 5, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "package:dep/src/helper.dart", "lang": "dart", "import_type": "import"},
				},
				"function_calls": []any{
					map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4, "lang": "dart"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/src/helper.dart",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"classes": []any{
					map[string]any{"name": "Helper", "line_number": 2, "end_line": 3, "uid": "uid:helper-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:helper-decoy")
}
