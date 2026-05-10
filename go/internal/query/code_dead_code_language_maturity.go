package query

const (
	deadCodeMaturityDerived          = "derived"
	deadCodeMaturityDerivedCandidate = "derived_candidate_only"
)

var deadCodeLanguageMaturity = map[string]string{
	"c":          deadCodeMaturityDerivedCandidate,
	"c_sharp":    deadCodeMaturityDerivedCandidate,
	"cpp":        deadCodeMaturityDerivedCandidate,
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
	"ruby":       deadCodeMaturityDerivedCandidate,
	"rust":       deadCodeMaturityDerived,
	"scala":      deadCodeMaturityDerivedCandidate,
	"swift":      deadCodeMaturityDerivedCandidate,
	"tsx":        deadCodeMaturityDerived,
	"typescript": deadCodeMaturityDerived,
}

var deadCodeLanguageExactnessBlockers = map[string][]string{
	"rust": {
		"macro_expansion_unavailable",
		"cfg_unresolved",
		"cargo_feature_resolution_unavailable",
		"semantic_module_resolution_unavailable",
		"trait_dispatch_unresolved",
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
