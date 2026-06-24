// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TypeScript receiver-constrained cross-file call-resolution goldens (#3156).
func TestCallResolutionGoldensTypeScript(t *testing.T) {
	t.Parallel()
	runCallResolutionGoldens(t, typeScriptCallResolutionGoldens())
}

func typeScriptCallResolutionGoldens() []callResolutionGolden {
	return []callResolutionGolden{
		// Receiver typed by an interface disambiguates a same-name method that
		// exists on two interfaces. The interface method resolves to the unique
		// implementer's method (inferred_obj_type → implementer → type_inferred).
		{
			name:           "interface_receiver_disambiguates_same_name_method",
			category:       categorySameNameMethods,
			wantCallee:     "uid:reader-impl-close",
			wantMethod:     codeprovenance.MethodTypeInferred,
			wantConfidence: 0.80,
			forbidCallees:  []string{"uid:writer-impl-close"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "ts-iface"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-iface", "relative_path": "main.ts",
					"parsed_file_data": map[string]any{
						"path":      "main.ts",
						"functions": []any{map[string]any{"name": "run", "line_number": 1, "end_line": 4, "uid": "uid:run"}},
						"function_calls": []any{
							map[string]any{"name": "close", "full_name": "r.close", "inferred_obj_type": "Reader", "line_number": 2, "lang": "typescript"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-iface", "relative_path": "reader.ts",
					"parsed_file_data": map[string]any{
						"path": "reader.ts",
						"interfaces": []any{
							map[string]any{"name": "Reader", "lang": "typescript", "line_number": 1, "end_line": 2, "uid": "uid:reader"},
						},
						"classes": []any{
							map[string]any{"name": "ReaderImpl", "lang": "typescript", "implemented_interfaces": []any{"Reader"}, "line_number": 3, "end_line": 6, "uid": "uid:reader-impl"},
						},
						"functions": []any{
							map[string]any{"name": "close", "class_context": "ReaderImpl", "lang": "typescript", "line_number": 4, "end_line": 5, "uid": "uid:reader-impl-close"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-iface", "relative_path": "writer.ts",
					"parsed_file_data": map[string]any{
						"path": "writer.ts",
						"interfaces": []any{
							map[string]any{"name": "Writer", "lang": "typescript", "line_number": 1, "end_line": 2, "uid": "uid:writer"},
						},
						"classes": []any{
							map[string]any{"name": "WriterImpl", "lang": "typescript", "implemented_interfaces": []any{"Writer"}, "line_number": 3, "end_line": 6, "uid": "uid:writer-impl"},
						},
						"functions": []any{
							map[string]any{"name": "close", "class_context": "WriterImpl", "lang": "typescript", "line_number": 4, "end_line": 5, "uid": "uid:writer-impl-close"},
						},
					},
				}},
			},
		},

		// Aliased named import binds the call to the imported target.
		{
			name:           "aliased_named_import_binds_callee",
			category:       categoryAlias,
			wantCallee:     "uid:lib-helper",
			wantMethod:     codeprovenance.MethodImportBinding,
			wantConfidence: 0.90,
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{
					"repo_id":     "ts-alias",
					"imports_map": map[string][]string{"helper": {"lib.ts"}},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-alias", "relative_path": "main.ts",
					"parsed_file_data": map[string]any{
						"path":      "main.ts",
						"functions": []any{map[string]any{"name": "caller", "line_number": 3, "end_line": 5, "uid": "uid:caller"}},
						"imports":   []any{map[string]any{"name": "helper", "alias": "h", "source": "./lib", "lang": "typescript"}},
						"function_calls": []any{
							map[string]any{"name": "h", "full_name": "h", "line_number": 4, "lang": "typescript"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-alias", "relative_path": "lib.ts",
					"parsed_file_data": map[string]any{
						"path":      "lib.ts",
						"functions": []any{map[string]any{"name": "helper", "line_number": 1, "end_line": 2, "uid": "uid:lib-helper"}},
					},
				}},
			},
		},

		// A call to a name that IS explicitly imported from an external module
		// (not in the repo) must not bind to an unrelated same-named local
		// symbol: TS blocks the direct-import → repo-unique fallback. Honest
		// non-resolution. The forbidden target is a real repo `helper`, so the
		// guard is meaningful (it would catch a shadowing false positive).
		{
			name:          "missing_dependency_named_import_unresolved",
			category:      categoryMissingDependency,
			forbidCallees: []string{"uid:local-helper"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{
					"repo_id":     "ts-missing",
					"imports_map": map[string][]string{"helper": {"external"}},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-missing", "relative_path": "main.ts",
					"parsed_file_data": map[string]any{
						"path":      "main.ts",
						"functions": []any{map[string]any{"name": "caller", "line_number": 3, "end_line": 5, "uid": "uid:caller"}},
						"imports":   []any{map[string]any{"name": "helper", "source": "./external", "lang": "typescript"}},
						"function_calls": []any{
							map[string]any{"name": "helper", "full_name": "helper", "line_number": 4, "lang": "typescript"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-missing", "relative_path": "util.ts",
					"parsed_file_data": map[string]any{
						"path":      "util.ts",
						"functions": []any{map[string]any{"name": "helper", "line_number": 1, "end_line": 2, "uid": "uid:local-helper"}},
					},
				}},
			},
		},

		// A dynamically computed import target (require of a non-literal) gives
		// no static binding; the call must not fabricate a match.
		{
			name:          "dynamic_require_unresolved",
			category:      categoryDynamicImport,
			forbidCallees: []string{"uid:plugin-run"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "ts-dynamic"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-dynamic", "relative_path": "main.ts",
					"parsed_file_data": map[string]any{
						"path":      "main.ts",
						"functions": []any{map[string]any{"name": "load", "line_number": 1, "end_line": 5, "uid": "uid:load"}},
						"function_calls": []any{
							map[string]any{"name": "run", "full_name": "mod.run", "line_number": 4, "lang": "typescript", "call_kind": "dynamic"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "ts-dynamic", "relative_path": "plugin.ts",
					"parsed_file_data": map[string]any{
						"path":       "plugin.ts",
						"interfaces": []any{map[string]any{"name": "Plugin", "line_number": 1, "end_line": 3, "uid": "uid:plugin"}},
						"functions":  []any{map[string]any{"name": "run", "class_context": "Plugin", "line_number": 2, "end_line": 2, "uid": "uid:plugin-run"}},
					},
				}},
			},
		},
	}
}
