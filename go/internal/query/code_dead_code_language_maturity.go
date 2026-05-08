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
	"rust":       deadCodeMaturityDerivedCandidate,
	"scala":      deadCodeMaturityDerivedCandidate,
	"swift":      deadCodeMaturityDerivedCandidate,
	"tsx":        deadCodeMaturityDerived,
	"typescript": deadCodeMaturityDerived,
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
