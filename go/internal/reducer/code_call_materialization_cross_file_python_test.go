package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesCrossFilePythonAliasedFromImports(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.py")
	calleePath := filepath.Join(repoRoot, "lib", "factory.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"create_app": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "app.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:python-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "create_app",
							"alias":  "make_app",
							"source": "lib.factory",
							"lang":   "python",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "make_app",
							"full_name":   "make_app",
							"line_number": 4,
							"lang":        "python",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "lib/factory.py",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "create_app",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:python-create-app",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:python-create-app"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesCrossFilePythonModuleAliasCalls(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.py")
	calleePath := filepath.Join(repoRoot, "pkg", "mod.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"run": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "app.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "main",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:python-main",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "pkg.mod",
							"alias":       "mod",
							"source":      "pkg.mod",
							"lang":        "python",
							"import_type": "import",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "run",
							"full_name":   "mod.run",
							"line_number": 4,
							"lang":        "python",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "pkg/mod.py",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    2,
							"uid":         "content-entity:python-run-target",
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:python-run-target"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesPythonClassAndInferredInstanceMethodCalls(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	appPath := filepath.Join(repoRoot, "app.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "app.py",
				"parsed_file_data": map[string]any{
					"path": appPath,
					"functions": []any{
						map[string]any{
							"name":        "lambda_handler",
							"line_number": 10,
							"end_line":    13,
							"uid":         "content-entity:python-lambda-handler",
						},
						map[string]any{
							"name":          "from_event",
							"class_context": "LogPartition",
							"line_number":   6,
							"end_line":      8,
							"uid":           "content-entity:python-log-partition-from-event",
						},
						map[string]any{
							"name":          "create_partition",
							"class_context": "LogProcessor",
							"line_number":   2,
							"end_line":      3,
							"uid":           "content-entity:python-log-processor-create-partition",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "from_event",
							"full_name":   "LogPartition.from_event",
							"line_number": 12,
							"lang":        "python",
						},
						map[string]any{
							"name":              "create_partition",
							"full_name":         "log_processor.create_partition",
							"inferred_obj_type": "LogProcessor",
							"line_number":       13,
							"lang":              "python",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v", got, want, rows)
	}

	callees := map[string]bool{}
	for _, row := range rows {
		if got, want := row["caller_entity_id"], "content-entity:python-lambda-handler"; got != want {
			t.Fatalf("caller_entity_id = %#v, want %#v; row=%#v", got, want, row)
		}
		callees[anyToString(row["callee_entity_id"])] = true
	}
	for _, want := range []string{
		"content-entity:python-log-partition-from-event",
		"content-entity:python-log-processor-create-partition",
	} {
		if !callees[want] {
			t.Fatalf("callee %q missing from rows %#v", want, rows)
		}
	}
}

func TestExtractCodeCallRowsResolvesPythonConstructorsInheritedClassMethodsAndClassReferences(
	t *testing.T,
) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-python", "graph_id": "repo-python"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "models.py",
				"parsed_file_data": map[string]any{
					"path": "models.py",
					"classes": []any{
						map[string]any{"uid": "class-base", "name": "BaseModel", "line_number": 1, "end_line": 1, "lang": "python"},
						map[string]any{"uid": "class-event", "name": "S3Event", "line_number": 5, "end_line": 10, "lang": "python", "bases": []any{"BaseModel"}},
						map[string]any{"uid": "class-worker", "name": "Worker", "line_number": 12, "end_line": 18, "lang": "python"},
					},
					"functions": []any{
						map[string]any{"uid": "fn-from-dict", "name": "from_dict", "line_number": 3, "end_line": 4, "lang": "python", "class_context": "BaseModel"},
						map[string]any{"uid": "fn-worker-init", "name": "__init__", "line_number": 13, "end_line": 14, "lang": "python", "class_context": "Worker"},
						map[string]any{"uid": "fn-main", "name": "main", "line_number": 20, "end_line": 22, "lang": "python"},
					},
					"function_calls": []any{
						map[string]any{"name": "Worker", "full_name": "Worker", "line_number": 21, "lang": "python", "call_kind": "constructor_call"},
						map[string]any{"name": "S3Event", "full_name": "S3Event", "line_number": 14, "lang": "python", "call_kind": "python.class_reference"},
						map[string]any{"name": "from_dict", "full_name": "S3Event.from_dict", "line_number": 14, "lang": "python"},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "fn-main", "class-worker")
	assertCodeCallRow(t, rows, "fn-main", "fn-worker-init")
	assertCodeCallRow(t, rows, "fn-worker-init", "class-event")
	assertCodeCallRow(t, rows, "fn-worker-init", "fn-from-dict")
}

func TestExtractCodeCallRowsResolvesPythonSelfMethodCalls(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-python",
			"relative_path": "processor.py",
			"parsed_file_data": map[string]any{
				"path":          "processor.py",
				"relative_path": "processor.py",
				"lang":          "python",
				"functions": []any{
					map[string]any{"uid": "fn-object", "name": "object", "line_number": 2, "end_line": 3, "lang": "python", "class_context": "LogProcessor"},
					map[string]any{"uid": "fn-create", "name": "create_partition", "line_number": 5, "end_line": 6, "lang": "python", "class_context": "LogProcessor"},
				},
				"function_calls": []any{
					map[string]any{
						"name":              "object",
						"full_name":         "self.object",
						"line_number":       6,
						"lang":              "python",
						"inferred_obj_type": "LogProcessor",
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows([]facts.Envelope{env})
	assertCodeCallRow(t, rows, "fn-create", "fn-object")
}

func assertCodeCallRow(t *testing.T, rows []map[string]any, callerID string, calleeID string) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) == callerID && anyToString(row["callee_entity_id"]) == calleeID {
			return
		}
	}
	t.Fatalf("missing code-call row %s -> %s in %#v", callerID, calleeID, rows)
}
