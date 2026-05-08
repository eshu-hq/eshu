package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPythonModuleAndFromImports(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "sample_projects", "sample_project"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	callerPath := filepath.Join(repoRoot, "module_a.py")
	calleePath := filepath.Join(repoRoot, "module_b.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"helper":       {calleePath},
					"process_data": {calleePath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "module_a.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "foo",
							"line_number": 5,
							"end_line":    6,
							"uid":         "content-entity:python-foo",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "module_b",
							"alias":       "mb",
							"source":      "./module_b",
							"lang":        "python",
							"import_type": "import",
						},
						map[string]any{
							"name":        "process_data",
							"source":      "./module_b",
							"lang":        "python",
							"import_type": "from",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":        "helper",
							"full_name":   "mb.helper",
							"line_number": 6,
							"lang":        "python",
						},
						map[string]any{
							"name":        "process_data",
							"full_name":   "process_data",
							"line_number": 6,
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
				"relative_path": "module_b.py",
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{
							"name":        "helper",
							"line_number": 3,
							"end_line":    4,
							"uid":         "content-entity:python-helper",
						},
						map[string]any{
							"name":        "process_data",
							"line_number": 5,
							"end_line":    6,
							"uid":         "content-entity:python-process-data",
						},
					},
				},
			},
		},
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)
	callerID := resolveContainingCodeEntityID(entityIndex, callerPath, "module_a.py", 6)
	if callerID == "" {
		t.Fatal("callerID = \"\", want non-empty")
	}
	fileData := envelopes[1].Payload["parsed_file_data"].(map[string]any)
	importTargets := codeCallImportedTargets(
		mapSlice(fileData["imports"]),
		map[string]any{
			"name":      "helper",
			"full_name": "mb.helper",
		},
	)
	if len(importTargets) != 1 {
		t.Fatalf("len(importTargets) = %d, want 1", len(importTargets))
	}
	if got, want := importTargets[0].symbolName, "helper"; got != want {
		t.Fatalf("importTargets[0].symbolName = %#v, want %#v", got, want)
	}
	if got, want := importTargets[0].importSource, "./module_b"; got != want {
		t.Fatalf("importTargets[0].importSource = %#v, want %#v", got, want)
	}
	if got := codeCallMatchImportedPath(
		callerPath,
		"module_a.py",
		importTargets[0].importSource,
		"python",
		repositoryImports["repo-python"][importTargets[0].symbolName],
	); got == "" {
		t.Fatal("codeCallMatchImportedPath() = \"\", want non-empty")
	}
	calleeID, calleeFile := resolveGenericCallee(
		entityIndex,
		"repo-python",
		repositoryImports["repo-python"],
		codeCallReexportIndex{},
		callerPath,
		"module_a.py",
		fileData,
		map[string]any{
			"name":        "helper",
			"full_name":   "mb.helper",
			"line_number": 6,
			"lang":        "python",
		},
	)
	if calleeID == "" {
		t.Fatal("calleeID = \"\", want non-empty")
	}
	if got, want := calleeFile, "module_b.py"; got != want {
		t.Fatalf("calleeFile = %#v, want %#v", got, want)
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	assertCodeCallRowByCalleeID(t, rows, "content-entity:python-helper", "module_b.py")
	assertCodeCallRowByCalleeID(t, rows, "content-entity:python-process-data", "module_b.py")
}

func assertCodeCallRowByCalleeID(
	t *testing.T,
	rows []map[string]any,
	calleeID string,
	wantCalleeFile string,
) {
	t.Helper()

	for _, row := range rows {
		if got, _ := row["callee_entity_id"].(string); got != calleeID {
			continue
		}
		if got, _ := row["callee_file"].(string); got != wantCalleeFile {
			t.Fatalf("callee_file = %#v, want %#v", got, wantCalleeFile)
		}
		return
	}
	t.Fatalf("rows missing callee_entity_id=%q in %#v", calleeID, rows)
}
