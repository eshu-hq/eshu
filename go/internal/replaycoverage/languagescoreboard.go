// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"sort"
	"strings"
)

// LanguageSurfacePrefix is the canonical coverage-key prefix for a language on
// the C-11 (#4364) language-parser scoreboard, e.g. "language:go". It namespaces
// language exemptions away from the blocking surface reconcile's "parser:" /
// "collector:" keys so the two never collide.
const LanguageSurfacePrefix = "language:"

// LanguageCoverageStatus is the scoreboard outcome for one ledger language.
type LanguageCoverageStatus string

const (
	// LanguageExempt means the language is exercised end-to-end by the
	// golden-corpus corpus (its files flow sync -> discover -> parse -> emit), so
	// a dedicated parser fixture would re-record a path the corpus already covers.
	LanguageExempt LanguageCoverageStatus = "exempt"
	// LanguageUncovered means the language has no parser-fixture replay scenario
	// and is not corpus-exercised: it is the C-12 (#4365) fixture-backfill worklist.
	LanguageUncovered LanguageCoverageStatus = "uncovered"
)

// LanguageCoverage is the scoreboard row for one ledger language.
type LanguageCoverage struct {
	// Language is the ledger language name.
	Language string `json:"language"`
	// Status is the scoreboard outcome (exempt or uncovered).
	Status LanguageCoverageStatus `json:"status"`
	// Reason is the audited exemption reason when Status is exempt; empty otherwise.
	Reason string `json:"reason,omitempty"`
}

// LanguageScoreboard is the C-11 (#4364) language-parser coverage scoreboard: the
// honest count of how many of the languages Eshu claims to parse are exercised by
// the golden-corpus corpus, plus the explicit uncovered list (the C-12 #4365
// worklist). It is a visibility-only artifact rendered into the C-7 dashboard and
// the JSON report; it deliberately does NOT feed the blocking surface reconcile,
// so the single Blocking severity knob stays the only gate control.
type LanguageScoreboard struct {
	// Total is the ledger language count (the denominator).
	Total int `json:"total"`
	// Exempt is the count of corpus-exercised languages.
	Exempt int `json:"exempt"`
	// Uncovered is the count of languages with no replay scenario yet.
	Uncovered int `json:"uncovered"`
	// PercentSatisfied is exempt/total, two decimals; 100 when the ledger is empty.
	PercentSatisfied float64 `json:"percent_satisfied"`
	// Languages are the per-language rows sorted by language name.
	Languages []LanguageCoverage `json:"languages"`
	// StaleExemptions are language_exemptions surfaces matching no ledger language
	// (a language was renamed or removed); sorted. They are reported, never
	// silently dropped, so the manifest cannot drift away from the ledger unseen.
	StaleExemptions []string `json:"stale_exemptions,omitempty"`
}

// BuildLanguageScoreboard classifies every language in the ledger against the
// manifest's language exemptions: a language with a matching exemption is exempt
// (with its audited reason), every other language is uncovered. Exemptions that
// match no ledger language are collected as stale drift. The output is
// deterministic: rows and stale entries are sorted.
func BuildLanguageScoreboard(ledger LanguageLedger, exemptions []Exemption) LanguageScoreboard {
	reasonByLanguage := map[string]string{}
	for _, ex := range exemptions {
		name := strings.TrimPrefix(ex.Surface, LanguageSurfacePrefix)
		reasonByLanguage[name] = ex.Reason
	}

	ledgerNames := map[string]struct{}{}
	board := LanguageScoreboard{Total: len(ledger.Languages)}
	for _, lang := range ledger.Languages {
		ledgerNames[lang.Language] = struct{}{}
		row := LanguageCoverage{Language: lang.Language}
		if reason, exempt := reasonByLanguage[lang.Language]; exempt {
			row.Status = LanguageExempt
			row.Reason = reason
			board.Exempt++
		} else {
			row.Status = LanguageUncovered
			board.Uncovered++
		}
		board.Languages = append(board.Languages, row)
	}
	sort.Slice(board.Languages, func(i, j int) bool {
		return board.Languages[i].Language < board.Languages[j].Language
	})

	for _, ex := range exemptions {
		name := strings.TrimPrefix(ex.Surface, LanguageSurfacePrefix)
		if _, ok := ledgerNames[name]; !ok {
			board.StaleExemptions = append(board.StaleExemptions, ex.Surface)
		}
	}
	sort.Strings(board.StaleExemptions)

	board.PercentSatisfied = satisfiedPercent(board.Exempt, board.Total)
	return board
}

// satisfiedPercent computes exempt-over-total as a two-decimal percentage. An
// empty ledger reports 100 so the empty scoreboard never renders a false 0.
func satisfiedPercent(satisfied, total int) float64 {
	if total == 0 {
		return 100
	}
	return float64(int((float64(satisfied)/float64(total))*10000+0.5)) / 100
}
