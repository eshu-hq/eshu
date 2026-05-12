package query

import (
	"slices"
	"strings"
)

const (
	deadCodeMaturityDerived          = "derived"
	deadCodeMaturityDerivedCandidate = "derived_candidate_only"
)

var deadCodeLanguageMaturity = map[string]string{
	"c":          deadCodeMaturityDerived,
	"c_sharp":    deadCodeMaturityDerived,
	"cpp":        deadCodeMaturityDerived,
	"dart":       deadCodeMaturityDerived,
	"elixir":     deadCodeMaturityDerived,
	"go":         deadCodeMaturityDerived,
	"groovy":     deadCodeMaturityDerivedCandidate,
	"haskell":    deadCodeMaturityDerived,
	"java":       deadCodeMaturityDerived,
	"javascript": deadCodeMaturityDerived,
	"kotlin":     deadCodeMaturityDerived,
	"perl":       deadCodeMaturityDerivedCandidate,
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

// deadCodeLanguageMaturityReport returns a copy so response construction cannot
// mutate the package-level dead-code support table.
func deadCodeLanguageMaturityReport() map[string]string {
	report := make(map[string]string, len(deadCodeLanguageMaturity))
	for language, maturity := range deadCodeLanguageMaturity {
		report[language] = maturity
	}
	return report
}

// deadCodeLanguageExactnessBlockerReport returns named blockers that prevent a
// language from claiming exact cleanup-safe dead-code truth.
func deadCodeLanguageExactnessBlockerReport() map[string][]string {
	report := make(map[string][]string, len(deadCodeLanguageExactnessBlockers))
	for language, blockers := range deadCodeLanguageExactnessBlockers {
		report[language] = append([]string(nil), blockers...)
	}
	return report
}

func deadCodeObservedExactnessBlockerReport(results []map[string]any) map[string][]string {
	observed := make(map[string]map[string]struct{})
	for _, result := range results {
		language := strings.ToLower(strings.TrimSpace(StringVal(result, "language")))
		if language == "" {
			continue
		}
		metadata, _ := result["metadata"].(map[string]any)
		for _, blocker := range StringSliceVal(metadata, "exactness_blockers") {
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
