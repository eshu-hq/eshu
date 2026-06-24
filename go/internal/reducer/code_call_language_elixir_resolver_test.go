// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallResolutionMethodElixirAliasImportBinding(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/worker.ex"
	calleePath := "/repo/lib/basic.ex"
	decoyPath := "/repo/lib/other.ex"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-elixir",
			"imports_map": map[string][]string{
				"Demo.Basic": {calleePath},
				"Demo.Other": {decoyPath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/worker.ex",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "class_context": "Demo.Worker", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Demo.Basic", "alias": "Basic", "lang": "elixir", "import_type": "alias"},
				},
				"function_calls": []any{
					map[string]any{"name": "greet", "full_name": "Basic.greet", "inferred_obj_type": "Basic", "line_number": 6, "lang": "elixir"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/basic.ex",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Demo.Basic", "line_number": 2, "end_line": 4, "uid": "uid:greet-basic"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/other.ex",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Demo.Other", "line_number": 2, "end_line": 4, "uid": "uid:greet-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:greet-basic"); got != codeprovenance.MethodImportBinding {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

func TestCodeCallResolutionMethodElixirAliasPrefixImportBinding(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/worker.ex"
	calleePath := "/repo/lib/context/basic.ex"
	decoyPath := "/repo/lib/context_basic_decoy.ex"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-elixir",
			"imports_map": map[string][]string{
				"Demo.Context.Basic": {calleePath},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/worker.ex",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "class_context": "Demo.Worker", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Demo.Context", "alias": "Context", "lang": "elixir", "import_type": "alias"},
				},
				"function_calls": []any{
					map[string]any{"name": "greet", "full_name": "Context.Basic.greet", "inferred_obj_type": "Context.Basic", "line_number": 6, "lang": "elixir"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/context/basic.ex",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Demo.Context.Basic", "line_number": 2, "end_line": 4, "uid": "uid:greet-basic"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/context_basic_decoy.ex",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Context.Basic", "line_number": 2, "end_line": 4, "uid": "uid:greet-decoy"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "uid:greet-basic"); got != codeprovenance.MethodImportBinding {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

func TestCodeCallElixirUnresolvedAliasBlocksRepoUniqueFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/worker.ex"
	decoyPath := "/repo/lib/basic_decoy.ex"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-elixir",
			"imports_map": map[string][]string{
				"Demo.Other": {"/repo/lib/other.ex"},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/worker.ex",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "class_context": "Demo.Worker", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Demo.Basic", "alias": "Basic", "lang": "elixir", "import_type": "alias"},
				},
				"function_calls": []any{
					map[string]any{"name": "greet", "full_name": "Basic.greet", "inferred_obj_type": "Basic", "line_number": 6, "lang": "elixir"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/basic_decoy.ex",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Basic", "line_number": 2, "end_line": 4, "uid": "uid:greet-decoy"},
				},
			},
		}},
	}

	assertCodeCallRowsDoNotTarget(t, envelopes, "uid:greet-decoy")
}

func TestCodeCallElixirUnresolvedAliasPrefixBlocksRepoUniqueFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/worker.ex"
	decoyPath := "/repo/lib/context_basic_decoy.ex"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-elixir",
			"imports_map": map[string][]string{
				"Demo.Other": {"/repo/lib/other.ex"},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/worker.ex",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "caller", "class_context": "Demo.Worker", "line_number": 5, "end_line": 7, "uid": "uid:caller"},
				},
				"imports": []any{
					map[string]any{"name": "Demo.Context", "alias": "Context", "lang": "elixir", "import_type": "alias"},
				},
				"function_calls": []any{
					map[string]any{"name": "greet", "full_name": "Context.Basic.greet", "inferred_obj_type": "Context.Basic", "line_number": 6, "lang": "elixir"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-elixir",
			"relative_path": "lib/context_basic_decoy.ex",
			"parsed_file_data": map[string]any{
				"path": decoyPath,
				"functions": []any{
					map[string]any{"name": "greet", "class_context": "Context.Basic", "line_number": 2, "end_line": 4, "uid": "uid:greet-decoy"},
				},
			},
		}},
	}

	assertCodeCallRowsDoNotTarget(t, envelopes, "uid:greet-decoy")
}

func assertCodeCallRowsDoNotTarget(t *testing.T, envelopes []facts.Envelope, calleeID string) {
	t.Helper()

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		if got := anyToString(row["callee_entity_id"]); got == calleeID {
			t.Fatalf("unresolved Elixir alias fell through to repo-unique decoy: %#v", rows)
		}
	}
}
