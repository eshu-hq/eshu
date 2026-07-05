// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import "testing"

func TestBuildLanguageScoreboardCountsParserFixturesAsSatisfied(t *testing.T) {
	ledger := LanguageLedger{Languages: []LanguageLedgerEntry{
		{Language: "cloudformation"}, {Language: "go"}, {Language: "rust"},
	}}
	exemptions := []Exemption{
		{Surface: "language:go", Reason: "exercised end-to-end by the golden-corpus 20-repo corpus"},
	}
	coverage := []CoverageEntry{
		{
			Surface:      "parser:cloudformation",
			Scenario:     ScenarioParserFixture,
			ScenarioType: ScenarioTypeBaseline,
			Ref:          "go/internal/replay/parserfixture/testdata/fixtures/cloudformation.fixture.json",
			ProofGate:    "parserfixture-tests",
		},
	}

	board := BuildLanguageScoreboard(ledger, exemptions, []SurfaceCoverage{{
		Surface:      SupportedSurface{Registry: RegistryParserLedger, Key: "parser:cloudformation"},
		ScenarioType: ScenarioTypeBaseline,
		Status:       StatusCovered,
		Scenario:     &coverage[0],
	}})

	if board.Total != 3 || board.Exempt != 1 || board.Fixture != 1 || board.Uncovered != 1 {
		t.Fatalf("board totals = %+v, want total=3 exempt=1 fixture=1 uncovered=1", board)
	}
	if board.PercentSatisfied != 66.67 {
		t.Errorf("percent satisfied = %.2f, want 66.67", board.PercentSatisfied)
	}
	byLang := map[string]LanguageCoverage{}
	for _, row := range board.Languages {
		byLang[row.Language] = row
	}
	if got := byLang["cloudformation"]; got.Status != LanguageFixture || got.Reason == "" {
		t.Errorf("cloudformation row = %+v, want fixture with reason", got)
	}
	if got := byLang["rust"]; got.Status != LanguageUncovered || got.Reason != "" {
		t.Errorf("rust row = %+v, want uncovered with no reason", got)
	}
}

func TestBuildLanguageScoreboardIgnoresUnresolvedParserFixtures(t *testing.T) {
	ledger := LanguageLedger{Languages: []LanguageLedgerEntry{{Language: "cloudformation"}}}
	entry := CoverageEntry{
		Surface:      "parser:cloudformation",
		Scenario:     ScenarioParserFixture,
		ScenarioType: ScenarioTypeBaseline,
		Ref:          "go/internal/replay/parserfixture/testdata/fixtures/missing.fixture.json",
		ProofGate:    "parserfixture-tests",
	}

	board := BuildLanguageScoreboard(ledger, nil, []SurfaceCoverage{{
		Surface:      SupportedSurface{Registry: RegistryParserLedger, Key: "parser:cloudformation"},
		ScenarioType: ScenarioTypeBaseline,
		Status:       StatusUnresolved,
		Scenario:     &entry,
	}})

	if board.Fixture != 0 || board.Uncovered != 1 {
		t.Fatalf("unresolved fixture row must stay uncovered, got %+v", board)
	}
	if got := board.Languages[0]; got.Status != LanguageUncovered {
		t.Fatalf("language row = %+v, want uncovered", got)
	}
}

func TestBuildLanguageScoreboardClassifiesExemptAndUncovered(t *testing.T) {
	ledger := LanguageLedger{Languages: []LanguageLedgerEntry{
		{Language: "go"}, {Language: "python"}, {Language: "rust"}, {Language: "c"},
	}}
	exemptions := []Exemption{
		{Surface: "language:go", Reason: "exercised end-to-end by the golden-corpus 20-repo corpus"},
		{Surface: "language:python", Reason: "exercised end-to-end by the golden-corpus 20-repo corpus"},
	}

	board := BuildLanguageScoreboard(ledger, exemptions, nil)

	if board.Total != 4 || board.Exempt != 2 || board.Uncovered != 2 {
		t.Fatalf("board totals = %+v, want total=4 exempt=2 uncovered=2", board)
	}
	if board.PercentSatisfied != 50 {
		t.Errorf("percent satisfied = %.2f, want 50.00", board.PercentSatisfied)
	}
	byLang := map[string]LanguageCoverage{}
	for _, row := range board.Languages {
		byLang[row.Language] = row
	}
	if got := byLang["go"]; got.Status != LanguageExempt || got.Reason == "" {
		t.Errorf("go row = %+v, want exempt with reason", got)
	}
	if got := byLang["rust"]; got.Status != LanguageUncovered || got.Reason != "" {
		t.Errorf("rust row = %+v, want uncovered with no reason", got)
	}
	if got := byLang["c"]; got.Status != LanguageUncovered {
		t.Errorf("c row = %+v, want uncovered", got)
	}
}

func TestBuildLanguageScoreboardEnumeratesEveryLedgerLanguage(t *testing.T) {
	// No language may be silently absent from the denominator: the scoreboard must
	// have exactly one row per ledger language regardless of exemptions.
	ledger := LanguageLedger{Languages: []LanguageLedgerEntry{
		{Language: "go"}, {Language: "rust"}, {Language: "swift"},
	}}
	board := BuildLanguageScoreboard(ledger, nil, nil)
	if board.Total != 3 || len(board.Languages) != 3 {
		t.Fatalf("scoreboard rows = %d total=%d, want 3 of each", len(board.Languages), board.Total)
	}
	if board.Exempt != 0 || board.Uncovered != 3 {
		t.Errorf("with no exemptions all languages must be uncovered; got exempt=%d uncovered=%d", board.Exempt, board.Uncovered)
	}
	// Deterministic, sorted by language name.
	for i := 1; i < len(board.Languages); i++ {
		if board.Languages[i-1].Language > board.Languages[i].Language {
			t.Fatalf("scoreboard not sorted at %d", i)
		}
	}
}

func TestBuildLanguageScoreboardFlagsStaleExemptions(t *testing.T) {
	// An exemption for a language not in the ledger (a rename/removal) is reported
	// as stale drift, never silently dropped.
	ledger := LanguageLedger{Languages: []LanguageLedgerEntry{{Language: "go"}}}
	board := BuildLanguageScoreboard(ledger, []Exemption{
		{Surface: "language:go", Reason: "corpus"},
		{Surface: "language:cobol", Reason: "ghost"},
	}, nil)
	if len(board.StaleExemptions) != 1 || board.StaleExemptions[0] != "language:cobol" {
		t.Fatalf("stale exemptions = %v, want [language:cobol]", board.StaleExemptions)
	}
	if board.Exempt != 1 || board.Uncovered != 0 {
		t.Errorf("stale exemption must not count toward exempt; got exempt=%d uncovered=%d", board.Exempt, board.Uncovered)
	}
}

func TestBuildLanguageScoreboardEmptyLedgerIsHundredPercent(t *testing.T) {
	board := BuildLanguageScoreboard(LanguageLedger{}, nil, nil)
	if board.Total != 0 || board.PercentSatisfied != 100 {
		t.Fatalf("empty scoreboard = %+v, want total=0 percent=100", board)
	}
}
