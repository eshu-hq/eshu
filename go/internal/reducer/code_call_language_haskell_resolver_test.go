package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallResolutionMethodHaskellQualifiedImportBinding(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/app/Main.hs"
	calleePath := "/repo/src/Data/Text.hs"
	decoyPath := "/repo/src/Other/Text.hs"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-haskell",
			"imports_map": map[string][]string{
				"Data.Text":  {calleePath},
				"Other.Text": {decoyPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-haskell",
			"relative_path": "app/Main.hs",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Data.Text", "alias": "T", "lang": "haskell"},
				},
				"function_calls": []any{
					map[string]any{"name": "pack", "full_name": "T.pack", "line_number": 6, "lang": "haskell"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-haskell",
			"relative_path": "src/Data/Text.hs",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "pack", "line_number": 2, "end_line": 3, "uid": "uid:pack"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-haskell",
			"relative_path": "src/Other/Text.hs",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "pack", "line_number": 2, "end_line": 3, "uid": "uid:pack-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:pack"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:pack-decoy")
}

func TestCodeCallHaskellUnresolvedQualifiedImportBlocksRepoUniqueFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/app/Main.hs"
	decoyPath := "/repo/src/LocalPack.hs"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id":     "repo-haskell",
			"imports_map": map[string][]string{},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-haskell",
			"relative_path": "app/Main.hs",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Data.Text", "alias": "T", "lang": "haskell"},
				},
				"function_calls": []any{
					map[string]any{"name": "pack", "full_name": "T.pack", "line_number": 6, "lang": "haskell"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-haskell",
			"relative_path": "src/LocalPack.hs",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "pack", "line_number": 2, "end_line": 3, "uid": "uid:pack-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "uid:caller", "uid:pack-decoy")
}
