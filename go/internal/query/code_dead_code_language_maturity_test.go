package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestHandleDeadCodeReportsLanguageMaturity(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "go-helper", "name": "goHelper", "labels": []any{"Function"},
						"file_path": "go/internal/query/helper.go", "repo_id": "repo-1", "repo_name": "eshu", "language": "go",
					},
					{
						"entity_id": "rust-helper", "name": "rust_helper", "labels": []any{"Function"},
						"file_path": "crates/eshu/src/lib.rs", "repo_id": "repo-1", "repo_name": "eshu", "language": "rust",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	analysis := data["analysis"].(map[string]any)
	maturity, ok := analysis["dead_code_language_maturity"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_maturity] type = %T, want map[string]any", analysis["dead_code_language_maturity"])
	}

	for language, want := range map[string]string{
		"c":          "derived",
		"c_sharp":    "derived",
		"cpp":        "derived",
		"go":         "derived",
		"python":     "derived",
		"javascript": "derived",
		"java":       "derived",
		"typescript": "derived",
		"tsx":        "derived",
		"rust":       "derived",
		"sql":        "derived",
		"ruby":       "derived",
		"groovy":     "derived_candidate_only",
		"php":        "derived",
	} {
		if got := maturity[language]; got != want {
			t.Fatalf("maturity[%s] = %#v, want %#v", language, got, want)
		}
	}

	blockers, ok := analysis["dead_code_language_exactness_blockers"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_exactness_blockers] type = %T, want map[string]any", analysis["dead_code_language_exactness_blockers"])
	}
	rustBlockers, ok := blockers["rust"].([]any)
	if !ok {
		t.Fatalf("blockers[rust] type = %T, want []any", blockers["rust"])
	}
	for _, want := range []string{
		"macro_expansion_unavailable",
		"cfg_unresolved",
		"cargo_feature_resolution_unavailable",
		"semantic_module_resolution_unavailable",
		"trait_dispatch_unresolved",
	} {
		if !queryTestStringSliceContains(rustBlockers, want) {
			t.Fatalf("blockers[rust] missing %q in %#v", want, rustBlockers)
		}
	}
	sqlBlockers, ok := blockers["sql"].([]any)
	if !ok {
		t.Fatalf("blockers[sql] type = %T, want []any", blockers["sql"])
	}
	for _, want := range []string{
		"dynamic_sql_unresolved",
		"dialect_specific_routine_resolution_unavailable",
		"migration_order_resolution_unavailable",
	} {
		if !queryTestStringSliceContains(sqlBlockers, want) {
			t.Fatalf("blockers[sql] missing %q in %#v", want, sqlBlockers)
		}
	}
	rubyBlockers, ok := blockers["ruby"].([]any)
	if !ok {
		t.Fatalf("blockers[ruby] type = %T, want []any", blockers["ruby"])
	}
	for _, want := range []string{
		"dynamic_dispatch_unresolved",
		"metaprogrammed_methods_unresolved",
		"autoload_resolution_unavailable",
		"framework_route_resolution_unavailable",
		"gem_public_api_surface_unresolved",
		"constant_resolution_unavailable",
	} {
		if !queryTestStringSliceContains(rubyBlockers, want) {
			t.Fatalf("blockers[ruby] missing %q in %#v", want, rubyBlockers)
		}
	}
	groovyBlockers, ok := blockers["groovy"].([]any)
	if !ok {
		t.Fatalf("blockers[groovy] type = %T, want []any", blockers["groovy"])
	}
	for _, want := range []string{
		"dynamic_dispatch_unresolved",
		"closure_delegate_resolution_unavailable",
		"jenkins_shared_library_resolution_unavailable",
		"pipeline_dsl_dynamic_steps_unresolved",
	} {
		if !queryTestStringSliceContains(groovyBlockers, want) {
			t.Fatalf("blockers[groovy] missing %q in %#v", want, groovyBlockers)
		}
	}
	phpBlockers, ok := blockers["php"].([]any)
	if !ok {
		t.Fatalf("blockers[php] type = %T, want []any", blockers["php"])
	}
	for _, want := range []string{
		"dynamic_dispatch_unresolved",
		"reflection_unresolved",
		"composer_autoload_resolution_unavailable",
		"include_require_resolution_unavailable",
		"framework_route_resolution_unavailable",
		"trait_resolution_unavailable",
		"namespace_alias_resolution_unavailable",
		"magic_method_dispatch_unresolved",
		"public_api_surface_unresolved",
	} {
		if !queryTestStringSliceContains(phpBlockers, want) {
			t.Fatalf("blockers[php] missing %q in %#v", want, phpBlockers)
		}
	}
	cBlockers, ok := blockers["c"].([]any)
	if !ok {
		t.Fatalf("blockers[c] type = %T, want []any", blockers["c"])
	}
	for _, want := range []string{
		"preprocessor_macro_expansion_unavailable",
		"conditional_compilation_unresolved",
		"build_target_resolution_unavailable",
		"include_graph_resolution_unavailable",
		"public_header_surface_unresolved",
		"function_pointer_dispatch_unresolved",
		"callback_registration_unresolved",
		"dynamic_symbol_lookup_unresolved",
		"external_linkage_resolution_unavailable",
	} {
		if !queryTestStringSliceContains(cBlockers, want) {
			t.Fatalf("blockers[c] missing %q in %#v", want, cBlockers)
		}
	}
	cppBlockers, ok := blockers["cpp"].([]any)
	if !ok {
		t.Fatalf("blockers[cpp] type = %T, want []any", blockers["cpp"])
	}
	for _, want := range []string{
		"preprocessor_macro_expansion_unavailable",
		"conditional_compilation_unresolved",
		"build_target_resolution_unavailable",
		"include_graph_resolution_unavailable",
		"public_header_surface_unresolved",
		"template_instantiation_unresolved",
		"overload_resolution_unavailable",
		"virtual_dispatch_unresolved",
		"function_pointer_dispatch_unresolved",
		"callback_registration_unresolved",
		"dynamic_symbol_lookup_unresolved",
		"external_linkage_resolution_unavailable",
	} {
		if !queryTestStringSliceContains(cppBlockers, want) {
			t.Fatalf("blockers[cpp] missing %q in %#v", want, cppBlockers)
		}
	}
	csharpBlockers, ok := blockers["c_sharp"].([]any)
	if !ok {
		t.Fatalf("blockers[c_sharp] type = %T, want []any", blockers["c_sharp"])
	}
	for _, want := range []string{
		"reflection_unresolved",
		"dependency_injection_resolution_unavailable",
		"source_generator_output_unavailable",
		"partial_type_resolution_unavailable",
		"dynamic_dispatch_unresolved",
		"project_reference_resolution_unavailable",
		"public_api_surface_unresolved",
	} {
		if !queryTestStringSliceContains(csharpBlockers, want) {
			t.Fatalf("blockers[c_sharp] missing %q in %#v", want, csharpBlockers)
		}
	}
}

func TestBuildDeadCodeAnalysisReportsObservedCExactnessBlockersFromGraphMetadata(t *testing.T) {
	t.Parallel()

	analysis := buildDeadCodeAnalysis([]map[string]any{
		{
			"entity_id": "c-dynamic-helper",
			"language":  "c",
			"metadata": map[string]any{
				"exactness_blockers": []any{
					"dynamic_symbol_lookup_unresolved",
					"function_pointer_dispatch_unresolved",
					"dynamic_symbol_lookup_unresolved",
				},
			},
		},
	}, nil, deadCodePolicyStats{})

	observed, ok := analysis["dead_code_observed_exactness_blockers"].(map[string][]string)
	if !ok {
		t.Fatalf("analysis[dead_code_observed_exactness_blockers] type = %T, want map[string][]string", analysis["dead_code_observed_exactness_blockers"])
	}
	if got, want := observed["c"], []string{"dynamic_symbol_lookup_unresolved", "function_pointer_dispatch_unresolved"}; !equalStringSlices(got, want) {
		t.Fatalf("observed[c] = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeReportsObservedRustExactnessBlockersFromContentMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "rust-helper", "name": "helper", "labels": []any{"Function"},
						"file_path": "src/lib.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "rust",
					},
					{
						"entity_id": "rust-worker", "name": "worker", "labels": []any{"Function"},
						"file_path": "src/worker.rs", "repo_id": "repo-1", "repo_name": "runtime", "language": "Rust",
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"rust-helper": {
					EntityID:     "rust-helper",
					RepoID:       "repo-1",
					RelativePath: "src/lib.rs",
					EntityType:   "Function",
					EntityName:   "helper",
					Language:     "rust",
					SourceCache:  "fn helper() {}",
					Metadata: map[string]any{
						"exactness_blockers": []any{
							"trait_dispatch_unresolved",
							"cfg_unresolved",
							"trait_dispatch_unresolved",
						},
					},
				},
				"rust-worker": {
					EntityID:     "rust-worker",
					RepoID:       "repo-1",
					RelativePath: "src/worker.rs",
					EntityType:   "Function",
					EntityName:   "worker",
					Language:     "Rust",
					SourceCache:  "fn worker() {}",
					Metadata: map[string]any{
						"exactness_blockers": []string{"macro_expansion_unavailable"},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	truth := resp["truth"].(map[string]any)
	if got, want := truth["level"], "derived"; got != want {
		t.Fatalf("truth[level] = %#v, want %#v", got, want)
	}
	data := resp["data"].(map[string]any)
	analysis := data["analysis"].(map[string]any)
	observed, ok := analysis["dead_code_observed_exactness_blockers"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_observed_exactness_blockers] type = %T, want map[string]any", analysis["dead_code_observed_exactness_blockers"])
	}
	assertQueryTestStringSliceEqual(t, observed["rust"], []string{
		"cfg_unresolved",
		"macro_expansion_unavailable",
		"trait_dispatch_unresolved",
	})
}

func TestBuildDeadCodeAnalysisReportsObservedRustExactnessBlockersFromGraphMetadata(t *testing.T) {
	t.Parallel()

	analysis := buildDeadCodeAnalysis([]map[string]any{
		{
			"entity_id": "rust-helper",
			"language":  "rust",
			"metadata": map[string]any{
				"exactness_blockers": []any{
					"trait_dispatch_unresolved",
					"cfg_unresolved",
					"cfg_unresolved",
				},
			},
		},
	}, nil, deadCodePolicyStats{})

	observed, ok := analysis["dead_code_observed_exactness_blockers"].(map[string][]string)
	if !ok {
		t.Fatalf("analysis[dead_code_observed_exactness_blockers] type = %T, want map[string][]string", analysis["dead_code_observed_exactness_blockers"])
	}
	if got, want := observed["rust"], []string{"cfg_unresolved", "trait_dispatch_unresolved"}; !equalStringSlices(got, want) {
		t.Fatalf("observed[rust] = %#v, want %#v", got, want)
	}
}

func TestDeadCodeLanguageMaturityCoversParserSourceLanguages(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	for _, definition := range registry.Definitions() {
		key := definition.ParserKey
		if !deadCodeSourceParserKeys[key] {
			if _, ok := deadCodeLanguageMaturity[key]; ok {
				t.Fatalf("deadCodeLanguageMaturity[%q] exists, want non-source parser excluded", key)
			}
			continue
		}
		if _, ok := deadCodeLanguageMaturity[key]; !ok {
			t.Fatalf("deadCodeLanguageMaturity missing source parser key %q", key)
		}
	}
}

var deadCodeSourceParserKeys = map[string]bool{
	"c":          true,
	"c_sharp":    true,
	"cpp":        true,
	"dart":       true,
	"elixir":     true,
	"go":         true,
	"groovy":     true,
	"haskell":    true,
	"java":       true,
	"javascript": true,
	"kotlin":     true,
	"perl":       true,
	"php":        true,
	"python":     true,
	"ruby":       true,
	"rust":       true,
	"scala":      true,
	"sql":        true,
	"swift":      true,
	"tsx":        true,
	"typescript": true,
}

func assertQueryTestStringSliceEqual(t *testing.T, got any, want []string) {
	t.Helper()

	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("string slice type = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
	}
	for i, wantValue := range want {
		if gotValue, ok := gotSlice[i].(string); !ok || gotValue != wantValue {
			t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
		}
	}
}
