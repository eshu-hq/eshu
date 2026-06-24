// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Go receiver-constrained cross-file call-resolution goldens (#3156).
func TestCallResolutionGoldensGo(t *testing.T) {
	t.Parallel()
	runCallResolutionGoldens(t, goCallResolutionGoldens())
}

func goCallResolutionGoldens() []callResolutionGolden {
	return []callResolutionGolden{
		// Same bare name defined in two packages; the caller's own directory
		// wins over the cross-directory homonym (scope_unique_name), and the
		// other-directory definition must not be chosen.
		{
			name:           "same_directory_wins_over_cross_dir_homonym",
			category:       categorySameNameMethods,
			wantCallee:     "uid:api-wire",
			wantMethod:     codeprovenance.MethodScopeUniqueName,
			wantConfidence: 0.70,
			forbidCallees:  []string{"uid:mcp-wire"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "go-scope"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-scope", "relative_path": "cmd/api/main.go",
					"parsed_file_data": map[string]any{
						"path":      "cmd/api/main.go",
						"functions": []any{map[string]any{"name": "main", "line_number": 1, "end_line": 9, "uid": "uid:api-main"}},
						"function_calls": []any{
							map[string]any{"name": "wireAPI", "full_name": "wireAPI", "line_number": 5, "lang": "go"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-scope", "relative_path": "cmd/api/wiring.go",
					"parsed_file_data": map[string]any{
						"path":      "cmd/api/wiring.go",
						"functions": []any{map[string]any{"name": "wireAPI", "line_number": 1, "end_line": 4, "uid": "uid:api-wire"}},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-scope", "relative_path": "cmd/mcp/wiring.go",
					"parsed_file_data": map[string]any{
						"path":      "cmd/mcp/wiring.go",
						"functions": []any{map[string]any{"name": "wireAPI", "line_number": 1, "end_line": 4, "uid": "uid:mcp-wire"}},
					},
				}},
			},
		},

		// A repository-wide unique bare name with no scope or import evidence is
		// the documented global fallback tier (repo_unique_name, 0.5).
		{
			name:           "repo_unique_bare_name_global_fallback",
			category:       categoryRepoFallback,
			wantCallee:     "uid:lone-helper",
			wantMethod:     codeprovenance.MethodRepoUniqueName,
			wantConfidence: 0.50,
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "go-unique"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-unique", "relative_path": "a/a.go",
					"parsed_file_data": map[string]any{
						"path":      "a/a.go",
						"functions": []any{map[string]any{"name": "caller", "line_number": 1, "end_line": 3, "uid": "uid:caller"}},
						"function_calls": []any{
							map[string]any{"name": "loneHelper", "full_name": "loneHelper", "line_number": 2, "lang": "go"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-unique", "relative_path": "b/b.go",
					"parsed_file_data": map[string]any{
						"path":      "b/b.go",
						"functions": []any{map[string]any{"name": "loneHelper", "line_number": 1, "end_line": 2, "uid": "uid:lone-helper"}},
					},
				}},
			},
		},

		// A call qualified by an external package selector whose package is not
		// in the repo must not be shadowed by an unrelated local bare name.
		{
			name:          "external_package_selector_not_shadowed",
			category:      categoryMissingDependency,
			forbidCallees: []string{"uid:local-do"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "go-missing"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-missing", "relative_path": "a/a.go",
					"parsed_file_data": map[string]any{
						"path":      "a/a.go",
						"functions": []any{map[string]any{"name": "caller", "line_number": 2, "end_line": 4, "uid": "uid:caller"}},
						"imports": []any{
							map[string]any{"name": "ext", "source": "github.com/third/ext", "lang": "go"},
						},
						"function_calls": []any{
							map[string]any{"name": "Do", "full_name": "ext.Do", "line_number": 3, "lang": "go"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "go-missing", "relative_path": "b/b.go",
					"parsed_file_data": map[string]any{
						"path":      "b/b.go",
						"functions": []any{map[string]any{"name": "Do", "line_number": 1, "end_line": 2, "uid": "uid:local-do"}},
					},
				}},
			},
		},
	}
}
