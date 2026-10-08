// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

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

		// A named import of another repository's package resolves through the
		// package key (#7601): the parser stamps package_export_symbol on the
		// call and package_id/export_name on the producer's export. The
		// consumer's own same-named helper is not the target.
		{
			name:           "package_import_resolves_across_repositories",
			category:       categoryMissingDependency,
			wantCallee:     "uid:format-formatPrice",
			wantMethod:     codeprovenance.MethodImportBinding,
			wantConfidence: 0.90,
			forbidCallees:  []string{"uid:app-formatPrice"},
			envelopes:      packageKeyGoldenEnvelopes("ts-format"),
		},

		// Two repositories publish the same package name, so the key names two
		// definitions and stays unresolved rather than picking one.
		{
			name:          "package_import_published_twice_unresolved",
			category:      categoryMissingDependency,
			forbidCallees: []string{"uid:format-formatPrice", "uid:fork-formatPrice"},
			envelopes:     packageKeyGoldenEnvelopes("ts-format", "ts-fork"),
		},

		// When the package key resolves to nothing (no producer, or two), the
		// call is bound to an explicitly imported but unresolvable target, so
		// it must not fall through to the repo-unique-name fallback and link
		// to the consumer's own same-named function in another file (#7610).
		// The key's package is not one of the consumer repository's own
		// manifest names, so the fallback stays blocked.
		{
			name:          "package_import_unresolved_falls_back_to_local_name",
			category:      categoryMissingDependency,
			forbidCallees: []string{"uid:app-formatPrice"},
			envelopes:     packageKeyGoldenEnvelopes(),
		},

		// The #7610 carve-out: the key's package is one of the consumer
		// repository's own manifest names (a same-repository workspace
		// package whose export list the parser does not key), so the
		// repo-unique fallback to the workspace definition is the true edge
		// and stays.
		{
			name:           "package_import_unresolved_workspace_package_falls_back",
			category:       categoryRepoFallback,
			wantCallee:     "uid:mono-widget",
			wantMethod:     codeprovenance.MethodRepoUniqueName,
			wantConfidence: 0.50,
			envelopes:      packageKeyWorkspaceGoldenEnvelopes(false),
		},

		// Same carve-out, but the workspace name is declared twice in the
		// repository: the fallback is allowed to try yet resolves nothing,
		// and neither same-named definition gains a wrong edge.
		{
			name:          "package_import_unresolved_workspace_package_ambiguous",
			category:      categoryRepoFallback,
			forbidCallees: []string{"uid:mono-widget", "uid:mono-decoy-widget"},
			envelopes:     packageKeyWorkspaceGoldenEnvelopes(true),
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

// packageKeyGoldenEnvelopes builds a consumer repository whose call is bound to
// the bare import "@acme/format", plus one producer repository per id that
// publishes formatPrice under that package name. The consumer also defines a
// formatPrice of its own in another file. The envelopes are hand-built, so the
// consumer's package.json is not parsed here; the parser stamps this call's
// package_export_symbol only when that manifest declares "@acme/format", which
// cross_repo_package_keys_test.go proves from real parser output. Both sides
// carry the node_package_name the parser would stamp (#7610): the consumer
// files belong to "acme-app", which publishes no "@acme/format".
func packageKeyGoldenEnvelopes(producerRepoIDs ...string) []facts.Envelope {
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "ts-app"}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id": "ts-app", "relative_path": "src/page.ts",
			"parsed_file_data": map[string]any{
				"path": "src/page.ts", "node_package_name": "acme-app",
				"functions": []any{map[string]any{"name": "render", "line_number": 3, "end_line": 5, "uid": "uid:app-render"}},
				"imports":   []any{map[string]any{"name": "formatPrice", "alias": "", "source": "@acme/format", "lang": "typescript"}},
				"function_calls": []any{map[string]any{
					"name": "formatPrice", "full_name": "formatPrice", "call_kind": "function_call",
					"line_number": 4, "lang": "typescript", "package_export_symbol": "package:@acme/format#formatPrice",
				}},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id": "ts-app", "relative_path": "src/money.ts",
			"parsed_file_data": map[string]any{
				"path": "src/money.ts", "node_package_name": "acme-app",
				"functions": []any{map[string]any{"name": "formatPrice", "line_number": 1, "end_line": 2, "uid": "uid:app-formatPrice"}},
			},
		}},
	}
	for _, repoID := range producerRepoIDs {
		uid := "uid:format-formatPrice"
		if repoID != "ts-format" {
			uid = "uid:fork-formatPrice"
		}
		envelopes = append(envelopes, facts.Envelope{FactKind: "file", Payload: map[string]any{
			"repo_id": repoID, "relative_path": "src/index.ts",
			"parsed_file_data": map[string]any{
				"path": "src/index.ts", "node_package_name": "@acme/format",
				"functions": []any{map[string]any{
					"name": "formatPrice", "line_number": 1, "end_line": 3, "uid": uid,
					"package_id": "@acme/format", "export_name": "formatPrice",
				}},
			},
		}})
	}
	return envelopes
}

// packageKeyWorkspaceGoldenEnvelopes builds a monorepo repository where the
// app file calls widget bound to "@acme/widgets" while the same repository
// publishes "@acme/widgets" with an export the parser does not key (an
// export list or a CommonJS module carries no package_id/export_name), so no
// symbol key joins the call. The widget definition is the fallback's true
// edge (#7610 carve-out). With ambiguous set, the app package declares a
// second widget, so the name is no longer repo-unique and the call must stay
// unresolved rather than guess.
func packageKeyWorkspaceGoldenEnvelopes(ambiguous bool) []facts.Envelope {
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "ts-mono"}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id": "ts-mono", "relative_path": "packages/app/src/page.ts",
			"parsed_file_data": map[string]any{
				"path": "packages/app/src/page.ts", "node_package_name": "@acme/app",
				"functions": []any{map[string]any{"name": "render", "line_number": 3, "end_line": 5, "uid": "uid:mono-render"}},
				"imports":   []any{map[string]any{"name": "widget", "alias": "", "source": "@acme/widgets", "lang": "typescript"}},
				"function_calls": []any{map[string]any{
					"name": "widget", "full_name": "widget", "call_kind": "function_call",
					"line_number": 4, "lang": "typescript", "package_export_symbol": "package:@acme/widgets#widget",
				}},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id": "ts-mono", "relative_path": "packages/widgets/index.ts",
			"parsed_file_data": map[string]any{
				"path": "packages/widgets/index.ts", "node_package_name": "@acme/widgets",
				"functions": []any{map[string]any{"name": "widget", "line_number": 1, "end_line": 3, "uid": "uid:mono-widget"}},
			},
		}},
	}
	if ambiguous {
		envelopes = append(envelopes, facts.Envelope{FactKind: "file", Payload: map[string]any{
			"repo_id": "ts-mono", "relative_path": "packages/app/src/decoy.ts",
			"parsed_file_data": map[string]any{
				"path": "packages/app/src/decoy.ts", "node_package_name": "@acme/app",
				"functions": []any{map[string]any{"name": "widget", "line_number": 1, "end_line": 2, "uid": "uid:mono-decoy-widget"}},
			},
		}})
	}
	return envelopes
}
