// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	deadCodeMaturityDerived          = "derived"
	deadCodeMaturityDerivedCandidate = "derived_candidate_only"
	deadCodeMaturityNonCodeIaC       = "non_code_iac_evidence"
)

// DeadCodeLanguageMaturity maps a normalized language to its dead-code
// modeling maturity. It is exported because the staying investigation and
// contract tests read through it via the root forward (same map).
var DeadCodeLanguageMaturity = map[string]string{
	"c":          deadCodeMaturityDerived,
	"c_sharp":    deadCodeMaturityDerived,
	"cpp":        deadCodeMaturityDerived,
	"dart":       deadCodeMaturityDerived,
	"elixir":     deadCodeMaturityDerived,
	"go":         deadCodeMaturityDerived,
	"groovy":     deadCodeMaturityDerivedCandidate,
	"hcl":        deadCodeMaturityNonCodeIaC,
	"haskell":    deadCodeMaturityDerived,
	"java":       deadCodeMaturityDerived,
	"javascript": deadCodeMaturityDerived,
	"kotlin":     deadCodeMaturityDerived,
	"perl":       deadCodeMaturityDerived,
	"php":        deadCodeMaturityDerived,
	"python":     deadCodeMaturityDerived,
	"ruby":       deadCodeMaturityDerived,
	"rust":       deadCodeMaturityDerived,
	"scala":      deadCodeMaturityDerived,
	"sql":        deadCodeMaturityDerived,
	"swift":      deadCodeMaturityDerived,
	"tsx":        deadCodeMaturityDerived,
	"typescript": deadCodeMaturityDerived,
}

var deadCodeLanguageExactnessBlockers = map[string][]string{
	"c": {
		"preprocessor_macro_expansion_unavailable",
		"conditional_compilation_unresolved",
		"build_target_resolution_unavailable",
		"include_graph_resolution_unavailable",
		"public_header_surface_unresolved",
		"function_pointer_dispatch_unresolved",
		"callback_registration_unresolved",
		"dynamic_symbol_lookup_unresolved",
		"external_linkage_resolution_unavailable",
	},
	"cpp": {
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
	},
	"c_sharp": {
		"reflection_unresolved",
		"dependency_injection_resolution_unavailable",
		"source_generator_output_unavailable",
		"partial_type_resolution_unavailable",
		"dynamic_dispatch_unresolved",
		"project_reference_resolution_unavailable",
		"public_api_surface_unresolved",
	},
	"dart": {
		"library_part_resolution_unavailable",
		"conditional_import_export_resolution_unavailable",
		"package_export_surface_unresolved",
		"dynamic_dispatch_unresolved",
		"flutter_route_resolution_unavailable",
		"generated_code_unavailable",
		"reflection_mirror_unresolved",
		"public_api_surface_unresolved",
	},
	"rust": {
		"macro_expansion_unavailable",
		"cfg_unresolved",
		"cargo_feature_resolution_unavailable",
		"semantic_module_resolution_unavailable",
		"trait_dispatch_unresolved",
	},
	"ruby": {
		"dynamic_dispatch_unresolved",
		"metaprogrammed_methods_unresolved",
		"autoload_resolution_unavailable",
		"framework_route_resolution_unavailable",
		"gem_public_api_surface_unresolved",
		"constant_resolution_unavailable",
	},
	"groovy": {
		"dynamic_dispatch_unresolved",
		"closure_delegate_resolution_unavailable",
		"jenkins_shared_library_resolution_unavailable",
		"pipeline_dsl_dynamic_steps_unresolved",
	},
	"haskell": {
		"template_haskell_expansion_unavailable",
		"cpp_conditional_compilation_unresolved",
		"cabal_component_resolution_unavailable",
		"implicit_module_export_surface_unresolved",
		"typeclass_dispatch_unresolved",
		"module_reexport_resolution_unavailable",
		"foreign_function_interface_unresolved",
	},
	"perl": {
		"symbolic_reference_dispatch_unresolved",
		"autoload_dispatch_unresolved",
		"inheritance_isa_resolution_unavailable",
		"moose_moo_metadata_unavailable",
		"import_side_effect_resolution_unavailable",
		"runtime_eval_unresolved",
		"public_api_surface_unresolved",
	},
	"hcl": {
		"terraform_plan_state_liveness_unavailable",
		"terraform_module_reference_graph_unresolved",
		"terraform_workspace_variable_resolution_unavailable",
		"terraform_dynamic_block_expansion_unavailable",
		"terragrunt_runtime_include_resolution_unavailable",
	},
	"php": {
		"dynamic_dispatch_unresolved",
		"reflection_unresolved",
		"composer_autoload_resolution_unavailable",
		"include_require_resolution_unavailable",
		"framework_route_resolution_unavailable",
		"trait_resolution_unavailable",
		"namespace_alias_resolution_unavailable",
		"magic_method_dispatch_unresolved",
		"public_api_surface_unresolved",
	},
	"kotlin": {
		"reflection_unresolved",
		"dependency_injection_resolution_unavailable",
		"annotation_processing_unavailable",
		"compiler_plugin_generated_code_unavailable",
		"dynamic_dispatch_unresolved",
		"gradle_source_set_resolution_unavailable",
		"multiplatform_target_resolution_unavailable",
		"public_api_surface_unresolved",
	},
	"scala": {
		"macro_expansion_unavailable",
		"implicit_resolution_unavailable",
		"given_using_resolution_unavailable",
		"dynamic_dispatch_unresolved",
		"reflection_unresolved",
		"sbt_source_set_resolution_unavailable",
		"framework_route_resolution_unavailable",
		"compiler_plugin_generated_code_unavailable",
		"public_api_surface_unresolved",
	},
	"elixir": {
		"macro_expansion_unavailable",
		"dynamic_dispatch_unresolved",
		"behaviour_callback_resolution_unavailable",
		"protocol_dispatch_unresolved",
		"phoenix_route_resolution_unavailable",
		"supervision_tree_resolution_unavailable",
		"mix_environment_resolution_unavailable",
		"public_api_surface_unresolved",
	},
	"sql": {
		"dynamic_sql_unresolved",
		"dialect_specific_routine_resolution_unavailable",
		"migration_order_resolution_unavailable",
	},
	"swift": {
		"macro_expansion_unavailable",
		"conditional_compilation_unresolved",
		"swiftpm_target_resolution_unavailable",
		"protocol_witness_resolution_unavailable",
		"dynamic_dispatch_unresolved",
		"property_wrapper_generated_code_unavailable",
		"result_builder_expansion_unavailable",
		"objective_c_runtime_dispatch_unresolved",
		"public_api_surface_unresolved",
	},
}

// DeadCodeLanguageMaturityReport returns a copy so response construction cannot
// mutate the package-level dead-code support table.
func DeadCodeLanguageMaturityReport() map[string]string {
	report := make(map[string]string, len(DeadCodeLanguageMaturity))
	for language, maturity := range DeadCodeLanguageMaturity {
		report[language] = maturity
	}
	return report
}

// DeadCodeLanguageExactnessBlockerReport returns named blockers that prevent a
// language from claiming exact cleanup-safe dead-code truth.
func DeadCodeLanguageExactnessBlockerReport() map[string][]string {
	report := make(map[string][]string, len(deadCodeLanguageExactnessBlockers))
	for language, blockers := range deadCodeLanguageExactnessBlockers {
		report[language] = append([]string(nil), blockers...)
	}
	return report
}

// deadCodeModeledFrameworks maps a normalized language to the frameworks for
// which dead-code has a root model: framework-specific root kinds the query
// honors (php.zf1_controller_action for zend_framework_1, route-backed kinds
// for laravel/slim/symfony, php.wordpress_hook_callback for wordpress).
// Languages without an entry are not evaluated: an observed framework stays
// silent rather than guessed. Extend the entry when a new framework root
// model lands.
//
// Census (#7712): go models net_http (registration roots plus
// go.net_http_handler_signature) and chi (no chi-specific kind, but chi
// handlers are net/http HandlerFuncs so the signature shape roots them;
// listing chi unmodeled would fire a false notice). gin/echo/fiber are
// observed with no root model, so they fire. cobra and controller-runtime
// have root models but no producer: adding one must add the table entry in
// the same change or it fires a false notice. groovy models jenkins, the
// only framework it emits. python models fastapi and flask (decorator
// roots); django/drf/aiohttp/tornado are observed with no root model, so
// they fire. celery/click/typer have decorator roots but no producer: the
// same same-change discipline as cobra applies.
var deadCodeModeledFrameworks = map[string]map[string]struct{}{
	"php": {
		"laravel":          {},
		"slim":             {},
		"symfony":          {},
		"wordpress":        {},
		"zend_framework_1": {},
	},
	"go": {
		"net_http": {},
		"chi":      {},
	},
	"groovy": {
		"jenkins": {},
	},
	"python": {
		"fastapi": {},
		"flask":   {},
	},
}

// deadCodeFrameworksWithoutRootModel reports, per normalized language, the
// observed result frameworks that have no dead-code root model. A framework
// is observed from result metadata; it is unmodeled when its language has a
// deadCodeModeledFrameworks entry that does not contain it.
func deadCodeFrameworksWithoutRootModel(results []map[string]any) map[string][]string {
	observed := make(map[string]map[string]struct{})
	for _, result := range results {
		language := normalizeDeadCodeLanguage(querycontract.StringVal(result, "language"))
		if language == "" {
			continue
		}
		modeled, ok := deadCodeModeledFrameworks[language]
		if !ok {
			continue
		}
		metadata, _ := result["metadata"].(map[string]any)
		framework := strings.ToLower(strings.TrimSpace(querycontract.StringVal(metadata, "framework")))
		if framework == "" {
			continue
		}
		if _, ok := modeled[framework]; ok {
			continue
		}
		if observed[language] == nil {
			observed[language] = make(map[string]struct{})
		}
		observed[language][framework] = struct{}{}
	}

	report := make(map[string][]string, len(observed))
	for language, frameworks := range observed {
		values := make([]string, 0, len(frameworks))
		for framework := range frameworks {
			values = append(values, framework)
		}
		slices.Sort(values)
		report[language] = values
	}
	return report
}

// deadCodeNoRootModelNote renders the human-readable notice for frameworks
// observed without a root model. Languages and frameworks render sorted so
// the note is deterministic.
func deadCodeNoRootModelNote(unmodeled map[string][]string) string {
	languages := make([]string, 0, len(unmodeled))
	for language := range unmodeled {
		languages = append(languages, language)
	}
	slices.Sort(languages)
	pairs := make([]string, 0, len(languages))
	for _, language := range languages {
		pairs = append(pairs, language+"("+strings.Join(unmodeled[language], ", ")+")")
	}
	return "dead-code has no root model for frameworks in use: " + strings.Join(pairs, ", ") +
		"; convention-dispatched entry points of those frameworks may be misreported as dead"
}

func deadCodeObservedExactnessBlockerReport(results []map[string]any) map[string][]string {
	observed := make(map[string]map[string]struct{})
	for _, result := range results {
		language := strings.ToLower(strings.TrimSpace(querycontract.StringVal(result, "language")))
		if language == "" {
			continue
		}
		metadata, _ := result["metadata"].(map[string]any)
		for _, blocker := range querycontract.StringSliceVal(metadata, "exactness_blockers") {
			blocker = strings.TrimSpace(blocker)
			if blocker == "" {
				continue
			}
			if observed[language] == nil {
				observed[language] = make(map[string]struct{})
			}
			observed[language][blocker] = struct{}{}
		}
	}

	report := make(map[string][]string, len(observed))
	for language, blockers := range observed {
		values := make([]string, 0, len(blockers))
		for blocker := range blockers {
			values = append(values, blocker)
		}
		slices.Sort(values)
		report[language] = values
	}
	return report
}
