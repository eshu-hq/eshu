package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsPrefersPythonAliasedImportBeforeRepoFallback(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.py")
	calleePath := filepath.Join(repoRoot, "lib", "factory.py")
	decoyPath := filepath.Join(repoRoot, "other.py")

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-python",
			"imports_map": map[string][]string{"create_app": {calleePath}},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-python",
			"relative_path": "app.py",
			"parsed_file_data": map[string]any{
				"path":      callerPath,
				"functions": []any{map[string]any{"name": "run", "line_number": 3, "end_line": 5, "uid": "content-entity:python-run"}},
				"imports": []any{map[string]any{
					"name": "create_app", "alias": "make_app", "source": "lib.factory", "lang": "python",
				}},
				"function_calls": []any{map[string]any{
					"name": "make_app", "full_name": "make_app", "line_number": 4, "lang": "python",
				}},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-python",
			"relative_path": "lib/factory.py",
			"parsed_file_data": map[string]any{
				"path":      calleePath,
				"functions": []any{map[string]any{"name": "create_app", "line_number": 1, "end_line": 2, "uid": "content-entity:python-create-app"}},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-python",
			"relative_path": "other.py",
			"parsed_file_data": map[string]any{
				"path":      decoyPath,
				"functions": []any{map[string]any{"name": "make_app", "line_number": 1, "end_line": 2, "uid": "content-entity:python-decoy-make-app"}},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v", got, want, rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:python-create-app"; got != want {
		t.Fatalf("callee_entity_id = %#v, want imported target %#v; rows=%#v", got, want, rows)
	}
	if got := resolutionMethodForCallee(t, rows, "content-entity:python-create-app"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertNoCodeCallRow(t, rows, "content-entity:python-run", "content-entity:python-decoy-make-app")
}
