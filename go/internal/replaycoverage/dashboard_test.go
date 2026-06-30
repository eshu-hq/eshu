// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"bytes"
	"strings"
	"testing"
)

// sampleReport builds a small reconciliation-derived report covering all four
// statuses for renderer tests.
func sampleReport(t *testing.T, blocking bool) CoverageReport {
	t.Helper()
	cov := Coverage{
		Surfaces: []SurfaceCoverage{
			{
				Surface:  SupportedSurface{Registry: RegistrySurfaceInventory, Key: "collector:aws"},
				Status:   StatusCovered,
				Scenario: &CoverageEntry{Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "testdata/cassettes/awscloud/x.json", ProofGate: "golden-corpus-gate"},
				Detail:   "artifact present",
			},
			{
				Surface:      SupportedSurface{Registry: RegistrySurfaceInventory, Key: "collector:webhook"},
				ScenarioType: ScenarioTypeFault,
				Status:       StatusUncovered,
				Detail:       "no replay scenario mapped for required scenario_type fault",
			},
			{
				Surface:  SupportedSurface{Registry: RegistryParserLedger, Key: "parser:hcl"},
				Status:   StatusCovered,
				Scenario: &CoverageEntry{Scenario: ScenarioParserFixture, ScenarioType: ScenarioTypeBaseline, Ref: "go/x/hcl.fixture.json", ProofGate: "parserfixture-tests"},
				Detail:   "artifact present",
			},
			{
				Surface:  SupportedSurface{Registry: RegistryFactKind, Key: "read_surface:GET /api/v0/images"},
				Status:   StatusUnresolved,
				Scenario: &CoverageEntry{Scenario: ScenarioAPIMCPGolden, ScenarioType: ScenarioTypeBaseline, Ref: "missing-shape", ProofGate: "golden-corpus-gate"},
				Detail:   "snapshot has no query shape",
			},
		},
	}
	return BuildReport(cov, blocking)
}

func TestRenderDashboardIsDeterministic(t *testing.T) {
	rep := sampleReport(t, false)
	a := RenderDashboard(rep)
	b := RenderDashboard(rep)
	if !bytes.Equal(a, b) {
		t.Fatal("RenderDashboard is not deterministic for the same report")
	}
}

func TestRenderDashboardShowsAxesGapsAndCovered(t *testing.T) {
	out := string(RenderDashboard(sampleReport(t, false)))

	for _, want := range []string{
		DashboardGeneratedMarker,
		"# Replay coverage",
		"## Coverage by axis",
		"## Coverage by scenario type",
		"Collectors",
		"Parsers",
		"Read surfaces (API/MCP)",
		"## Gaps — surfaces still needing a replay scenario",
		"`collector:webhook`",               // an uncovered surface is named
		"`read_surface:GET /api/v0/images`", // an unresolved surface is named
		"_unresolved: manifest entry present but artifact missing_",
		"## Covered surfaces",
		"`collector:aws`",
		"baseline",
		"fault",
		"golden-corpus-gate",
		"`testdata/cassettes/awscloud/x.json`",
		"mode: advisory",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestRenderDashboardBlockingModeAndCounts(t *testing.T) {
	out := string(RenderDashboard(sampleReport(t, true)))
	if !strings.Contains(out, "mode: blocking") {
		t.Error("blocking report must render mode: blocking")
	}
	// 4 surfaces, 2 covered+exempt, 2 gaps (1 uncovered + 1 unresolved).
	if !strings.Contains(out, "2/4 surfaces satisfied") {
		t.Errorf("overall tally wrong; got:\n%s", out)
	}
	if !strings.Contains(out, "2 surface(s) uncovered or unresolved") {
		t.Errorf("gap count wrong; got:\n%s", out)
	}
}

func TestRenderDashboardAllCoveredCelebrates(t *testing.T) {
	cov := Coverage{Surfaces: []SurfaceCoverage{{
		Surface:  SupportedSurface{Registry: RegistryParserLedger, Key: "parser:hcl"},
		Status:   StatusCovered,
		Scenario: &CoverageEntry{Scenario: ScenarioParserFixture, ScenarioType: ScenarioTypeBaseline, Ref: "x", ProofGate: "parserfixture-tests"},
	}}}
	out := string(RenderDashboard(BuildReport(cov, true)))
	if !strings.Contains(out, "Every supported surface has a replay scenario") {
		t.Errorf("all-covered dashboard should show the no-gaps message; got:\n%s", out)
	}
}

func TestRenderDashboardLanguageScoreboardSection(t *testing.T) {
	rep := sampleReport(t, true)
	rep.LanguageScoreboard = BuildLanguageScoreboard(
		LanguageLedger{Languages: []LanguageLedgerEntry{{Language: "go"}, {Language: "rust"}, {Language: "c"}}},
		[]Exemption{{Surface: "language:go", Reason: "exercised end-to-end by the golden-corpus 20-repo corpus"}},
	)
	out := string(RenderDashboard(rep))
	for _, want := range []string{
		"## Language parser coverage",
		"1/3 languages exercised by the corpus",
		"Uncovered (2)",
		"`rust`",
		"`c`",
		"#4365", // the C-12 worklist pointer
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scoreboard dashboard missing %q", want)
		}
	}
	// An exempt language is not listed as uncovered.
	if strings.Contains(out, "- `go`") {
		t.Error("exempt language go must not appear in the uncovered list")
	}
}

func TestRenderDashboardOmitsEmptyLanguageScoreboard(t *testing.T) {
	// Renderer-only reports (no ledger loaded) must not emit the section.
	out := string(RenderDashboard(sampleReport(t, false)))
	if strings.Contains(out, "## Language parser coverage") {
		t.Error("empty language scoreboard must be omitted, not rendered")
	}
}
