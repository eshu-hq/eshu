package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// resolutionMethodForCallee returns the resolution_method on the materialized
// code-call row whose callee is calleeID, failing if no such row exists.
func resolutionMethodForCallee(t *testing.T, rows []map[string]any, calleeID string) string {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == calleeID {
			return anyToString(row["resolution_method"])
		}
	}
	t.Fatalf("no code-call row for callee %q (rows=%d)", calleeID, len(rows))
	return ""
}

func TestCodeCallResolutionMethodSCIP(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/caller.py"
	calleePath := "/repo/callee.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-scip",
			"relative_path": "caller.py",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 3, "uid": "uid:caller"},
				},
				"function_calls_scip": []any{
					map[string]any{
						"caller_file": callerPath, "caller_line": 1,
						"callee_file": calleePath, "callee_line": 1,
						"ref_line": 2,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-scip",
			"relative_path": "callee.py",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "target", "line_number": 1, "end_line": 3, "uid": "uid:target"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:target"); got != codeprovenance.MethodSCIP {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodSCIP)
	}
}

func TestCodeCallResolutionMethodSameFile(t *testing.T) {
	t.Parallel()

	path := "/repo/mod.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-same-file",
			"relative_path": "mod.py",
			"parsed_file_data": map[string]any{
				"path": path,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 4, "uid": "uid:caller"},
					map[string]any{"name": "helper", "line_number": 6, "end_line": 7, "uid": "uid:helper"},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "line_number": 2, "lang": "python"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:helper"); got != codeprovenance.MethodSameFile {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodSameFile)
	}
}

func TestCodeCallResolutionMethodImportBinding(t *testing.T) {
	t.Parallel()

	// process_data is defined in two modules, so the bare name is ambiguous
	// repo-wide and the repo_unique_name tier cannot fire. The explicit import
	// from ./module_b is what disambiguates the call, so the edge is
	// import-bound.
	callerPath := "/repo/module_a.py"
	calleeBPath := "/repo/module_b.py"
	calleeCPath := "/repo/module_c.py"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-import",
			"imports_map": map[string][]string{"process_data": {calleeBPath}},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-import",
			"relative_path": "module_a.py",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "foo", "line_number": 5, "end_line": 6, "uid": "uid:foo"},
				},
				"imports": []any{
					map[string]any{"name": "process_data", "source": "./module_b", "lang": "python", "import_type": "from"},
				},
				"function_calls": []any{
					map[string]any{"name": "process_data", "full_name": "process_data", "line_number": 6, "lang": "python"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-import",
			"relative_path": "module_b.py",
			"parsed_file_data": map[string]any{
				"path": calleeBPath,
				"functions": []any{
					map[string]any{"name": "process_data", "line_number": 5, "end_line": 6, "uid": "uid:process-data-b"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-import",
			"relative_path": "module_c.py",
			"parsed_file_data": map[string]any{
				"path": calleeCPath,
				"functions": []any{
					map[string]any{"name": "process_data", "line_number": 5, "end_line": 6, "uid": "uid:process-data-c"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:process-data-b"); got != codeprovenance.MethodImportBinding {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

func TestCodeCallResolutionMethodRepoUniqueName(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/a.py"
	calleePath := "/repo/sub/b.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-unique",
			"relative_path": "a.py",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 4, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{"name": "loneHelper", "full_name": "loneHelper", "line_number": 2, "lang": "python"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-unique",
			"relative_path": "sub/b.py",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "loneHelper", "line_number": 1, "end_line": 3, "uid": "uid:lone"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:lone"); got != codeprovenance.MethodRepoUniqueName {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodRepoUniqueName)
	}
}

func TestCodeCallResolutionMethodTypeInferredConstructor(t *testing.T) {
	t.Parallel()

	appPath := "/repo/app.py"
	widgetPath := "/repo/widget.py"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-ctor",
			"relative_path": "app.py",
			"parsed_file_data": map[string]any{
				"path": appPath,
				"functions": []any{
					map[string]any{"name": "build", "line_number": 1, "end_line": 5, "uid": "uid:build"},
				},
				"function_calls": []any{
					map[string]any{"name": "Widget", "full_name": "Widget", "call_kind": "constructor_call", "line_number": 2, "lang": "python"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-ctor",
			"relative_path": "widget.py",
			"parsed_file_data": map[string]any{
				"path": widgetPath,
				"classes": []any{
					map[string]any{"name": "Widget", "line_number": 1, "end_line": 4, "uid": "uid:widget-class"},
				},
				"functions": []any{
					map[string]any{"name": "__init__", "class_context": "Widget", "line_number": 2, "end_line": 3, "uid": "uid:widget-init"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	// The secondary constructor edge to __init__ is type-inferred.
	if got := resolutionMethodForCallee(t, rows, "uid:widget-init"); got != codeprovenance.MethodTypeInferred {
		t.Errorf("constructor edge resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
}

func TestCodeCallResolutionMethodScopeUniqueNameGo(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/pkg/a.go"
	calleePath := "/repo/pkg/b.go"
	envelopes := []facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-go-dir",
			"relative_path": "pkg/a.go",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 4, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 2, "lang": "go"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-go-dir",
			"relative_path": "pkg/b.go",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "Helper", "line_number": 1, "end_line": 3, "uid": "uid:helper"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:helper"); got != codeprovenance.MethodScopeUniqueName {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodScopeUniqueName)
	}
}

func TestPythonMetaclassRowsCarryDeclaredResolutionMethod(t *testing.T) {
	t.Parallel()

	widgetPath := "/repo/widget.py"
	metaPath := "/repo/meta.py"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-meta",
			"imports_map": map[string][]string{"MetaLogger": {metaPath}},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-meta",
			"relative_path": "widget.py",
			"parsed_file_data": map[string]any{
				"path": widgetPath,
				"classes": []any{
					map[string]any{"name": "Widget", "metaclass": "MetaLogger", "line_number": 1, "end_line": 3, "uid": "uid:widget"},
				},
				"imports": []any{
					map[string]any{"name": "MetaLogger", "source": "./meta", "lang": "python", "import_type": "from"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-meta",
			"relative_path": "meta.py",
			"parsed_file_data": map[string]any{
				"path": metaPath,
				"classes": []any{
					map[string]any{"name": "MetaLogger", "line_number": 1, "end_line": 3, "uid": "uid:meta"},
				},
			},
		}},
	}

	_, _, _, metaclassRows := ExtractAllCodeRelationshipRows(envelopes)
	if len(metaclassRows) != 1 {
		t.Fatalf("len(metaclassRows) = %d, want 1", len(metaclassRows))
	}
	if got := anyToString(metaclassRows[0]["resolution_method"]); got != codeprovenance.MethodDeclared {
		t.Errorf("metaclass resolution_method = %q, want %q", got, codeprovenance.MethodDeclared)
	}
}
