package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Python receiver-constrained cross-file call-resolution goldens (#3156).
func TestCallResolutionGoldensPython(t *testing.T) {
	t.Parallel()
	runCallResolutionGoldens(t, pythonCallResolutionGoldens())
}

func pythonCallResolutionGoldens() []callResolutionGolden {
	return []callResolutionGolden{
		// Same-name method on two classes; the qualified receiver disambiguates.
		{
			name:           "qualified_receiver_disambiguates_same_name_method",
			category:       categorySameNameMethods,
			wantCallee:     "uid:userrepo-save",
			wantMethod:     codeprovenance.MethodTypeInferred,
			wantConfidence: 0.80,
			forbidCallees:  []string{"uid:orderrepo-save"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "py-same-name"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-same-name", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path": "/repo/app.py",
						"functions": []any{
							map[string]any{"name": "run", "line_number": 1, "end_line": 3, "uid": "uid:run"},
						},
						"function_calls": []any{
							map[string]any{"name": "save", "full_name": "UserRepo.save", "line_number": 2, "lang": "python"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-same-name", "relative_path": "user.py",
					"parsed_file_data": map[string]any{
						"path":    "/repo/user.py",
						"classes": []any{map[string]any{"name": "UserRepo", "line_number": 1, "end_line": 4, "uid": "uid:userrepo"}},
						"functions": []any{
							map[string]any{"name": "save", "class_context": "UserRepo", "line_number": 2, "end_line": 3, "uid": "uid:userrepo-save"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-same-name", "relative_path": "order.py",
					"parsed_file_data": map[string]any{
						"path":    "/repo/order.py",
						"classes": []any{map[string]any{"name": "OrderRepo", "line_number": 1, "end_line": 4, "uid": "uid:orderrepo"}},
						"functions": []any{
							map[string]any{"name": "save", "class_context": "OrderRepo", "line_number": 2, "end_line": 3, "uid": "uid:orderrepo-save"},
						},
					},
				}},
			},
		},

		// Bare same-name method call with no receiver and two candidates must NOT
		// resolve to either: ambiguity is honest non-resolution, not a guess.
		{
			name:          "bare_ambiguous_same_name_method_unresolved",
			category:      categoryReceiverAmbiguity,
			forbidCallees: []string{"uid:userrepo-save", "uid:orderrepo-save"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "py-ambig"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-ambig", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/app.py",
						"functions": []any{map[string]any{"name": "run", "line_number": 1, "end_line": 3, "uid": "uid:run"}},
						"function_calls": []any{
							map[string]any{"name": "save", "full_name": "save", "line_number": 2, "lang": "python"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-ambig", "relative_path": "user.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/user.py",
						"classes":   []any{map[string]any{"name": "UserRepo", "line_number": 1, "end_line": 4, "uid": "uid:userrepo"}},
						"functions": []any{map[string]any{"name": "save", "class_context": "UserRepo", "line_number": 2, "end_line": 3, "uid": "uid:userrepo-save"}},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-ambig", "relative_path": "order.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/order.py",
						"classes":   []any{map[string]any{"name": "OrderRepo", "line_number": 1, "end_line": 4, "uid": "uid:orderrepo"}},
						"functions": []any{map[string]any{"name": "save", "class_context": "OrderRepo", "line_number": 2, "end_line": 3, "uid": "uid:orderrepo-save"}},
					},
				}},
			},
		},

		// Aliased from-import binds the call to the imported target.
		{
			name:           "aliased_from_import_binds_callee",
			category:       categoryAlias,
			wantCallee:     "uid:create-app",
			wantMethod:     codeprovenance.MethodImportBinding,
			wantConfidence: 0.90,
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{
					"repo_id":     "py-alias",
					"imports_map": map[string][]string{"create_app": {"/repo/lib/factory.py"}},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-alias", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path":           "/repo/app.py",
						"functions":      []any{map[string]any{"name": "run", "line_number": 3, "end_line": 5, "uid": "uid:run"}},
						"imports":        []any{map[string]any{"name": "create_app", "alias": "make_app", "source": "lib.factory", "lang": "python"}},
						"function_calls": []any{map[string]any{"name": "make_app", "full_name": "make_app", "line_number": 4, "lang": "python"}},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-alias", "relative_path": "lib/factory.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/lib/factory.py",
						"functions": []any{map[string]any{"name": "create_app", "line_number": 1, "end_line": 2, "uid": "uid:create-app"}},
					},
				}},
			},
		},

		// Explicit from-package import of a repo-unique symbol. Current
		// resolution finds the correct target but labels it repo_unique_name
		// (0.5); the explicit import makes import_binding (0.9) the precision
		// ideal. Conservative gap (correct target, weaker provenance), tracked.
		{
			name:           "reexport_through_init_binds_callee",
			category:       categoryReexport,
			wantCallee:     "uid:impl-handler",
			wantMethod:     codeprovenance.MethodRepoUniqueName,
			wantConfidence: 0.50,
			idealMethod:    codeprovenance.MethodImportBinding,
			gapIssue:       "#3198",
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{
					"repo_id":     "py-reexport",
					"imports_map": map[string][]string{"handler": {"/repo/pkg/impl.py"}},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-reexport", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path":           "/repo/app.py",
						"functions":      []any{map[string]any{"name": "run", "line_number": 2, "end_line": 4, "uid": "uid:run"}},
						"imports":        []any{map[string]any{"name": "handler", "source": "pkg", "lang": "python"}},
						"function_calls": []any{map[string]any{"name": "handler", "full_name": "handler", "line_number": 3, "lang": "python"}},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-reexport", "relative_path": "pkg/impl.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/pkg/impl.py",
						"functions": []any{map[string]any{"name": "handler", "line_number": 1, "end_line": 2, "uid": "uid:impl-handler"}},
					},
				}},
			},
		},

		// Dynamic import (importlib) gives no static binding; must not fabricate.
		{
			name:          "dynamic_importlib_call_unresolved",
			category:      categoryDynamicImport,
			forbidCallees: []string{"uid:plugin-run"},
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "py-dynamic"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-dynamic", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/app.py",
						"functions": []any{map[string]any{"name": "run", "line_number": 1, "end_line": 4, "uid": "uid:run"}},
						"function_calls": []any{
							map[string]any{"name": "run", "full_name": "mod.run", "line_number": 3, "lang": "python", "call_kind": "dynamic"},
						},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-dynamic", "relative_path": "plugin.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/plugin.py",
						"classes":   []any{map[string]any{"name": "Plugin", "line_number": 1, "end_line": 4, "uid": "uid:plugin"}},
						"functions": []any{map[string]any{"name": "run", "class_context": "Plugin", "line_number": 2, "end_line": 3, "uid": "uid:plugin-run"}},
					},
				}},
			},
		},

		// Import from a dependency not present in the repo must not fabricate a
		// match against an unrelated same-named local symbol. Current behavior
		// IS a false positive here (repo-unique fallback shadows the external
		// import); documented and tracked by #3198 until the resolver is fixed.
		{
			name:             "missing_dependency_import_unresolved",
			category:         categoryMissingDependency,
			forbidCallees:    []string{"uid:local-connect"},
			falsePositiveGap: "#3198",
			envelopes: []facts.Envelope{
				{FactKind: "repository", Payload: map[string]any{"repo_id": "py-missing"}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-missing", "relative_path": "app.py",
					"parsed_file_data": map[string]any{
						"path":           "/repo/app.py",
						"functions":      []any{map[string]any{"name": "run", "line_number": 2, "end_line": 4, "uid": "uid:run"}},
						"imports":        []any{map[string]any{"name": "connect", "source": "third_party.db", "lang": "python"}},
						"function_calls": []any{map[string]any{"name": "connect", "full_name": "connect", "line_number": 3, "lang": "python"}},
					},
				}},
				{FactKind: "file", Payload: map[string]any{
					"repo_id": "py-missing", "relative_path": "internal/util.py",
					"parsed_file_data": map[string]any{
						"path":      "/repo/internal/util.py",
						"functions": []any{map[string]any{"name": "connect", "line_number": 1, "end_line": 2, "uid": "uid:local-connect"}},
					},
				}},
			},
		},
	}
}
