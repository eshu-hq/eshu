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
	"c_sharp":    deadCodeMaturityDerivedCandidate,
	"cpp":        deadCodeMaturityDerived,
	"dart":       deadCodeMaturityDerivedCandidate,
	"elixir":     deadCodeMaturityDerivedCandidate,
	"go":         deadCodeMaturityDerived,
	"groovy":     deadCodeMaturityDerivedCandidate,
	"haskell":    deadCodeMaturityDerivedCandidate,
	"java":       deadCodeMaturityDerived,
	"javascript": deadCodeMaturityDerived,
	"kotlin":     deadCodeMaturityDerivedCandidate,
	"perl":       deadCodeMaturityDerivedCandidate,
	"php":        deadCodeMaturityDerivedCandidate,
	"python":     deadCodeMaturityDerived,
	"ruby":       deadCodeMaturityDerived,
	"rust":       deadCodeMaturityDerived,
	"scala":      deadCodeMaturityDerivedCandidate,
	"sql":        deadCodeMaturityDerived,
	"swift":      deadCodeMaturityDerivedCandidate,
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
	"sql": {
		"dynamic_sql_unresolved",
		"dialect_specific_routine_resolution_unavailable",
		"migration_order_resolution_unavailable",
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
