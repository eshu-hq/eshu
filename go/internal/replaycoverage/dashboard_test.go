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
				Scenario: &CoverageEntry{Scenario: ScenarioCassette, Ref: "testdata/cassettes/awscloud/x.json", ProofGate: "golden-corpus-gate"},
				Detail:   "artifact present",
			},
			{
				Surface: SupportedSurface{Registry: RegistrySurfaceInventory, Key: "collector:webhook"},
				Status:  StatusUncovered,
				Detail:  "no replay scenario mapped",
			},
			{
				Surface:  SupportedSurface{Registry: RegistryParserLedger, Key: "parser:hcl"},
				Status:   StatusCovered,
				Scenario: &CoverageEntry{Scenario: ScenarioParserFixture, Ref: "go/x/hcl.fixture.json", ProofGate: "parserfixture-tests"},
				Detail:   "artifact present",
			},
			{
				Surface:  SupportedSurface{Registry: RegistryFactKind, Key: "read_surface:GET /api/v0/images"},
				Status:   StatusUnresolved,
				Scenario: &CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "missing-shape", ProofGate: "golden-corpus-gate"},
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
		"Collectors",
		"Parsers",
		"Read surfaces (API/MCP)",
		"## Gaps — surfaces still needing a replay scenario",
		"`collector:webhook`",               // an uncovered surface is named
		"`read_surface:GET /api/v0/images`", // an unresolved surface is named
		"_unresolved: manifest entry present but artifact missing_",
		"## Covered surfaces",
		"`collector:aws`",
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
		Scenario: &CoverageEntry{Scenario: ScenarioParserFixture, Ref: "x", ProofGate: "parserfixture-tests"},
	}}}
	out := string(RenderDashboard(BuildReport(cov, true)))
	if !strings.Contains(out, "Every supported surface has a replay scenario") {
		t.Errorf("all-covered dashboard should show the no-gaps message; got:\n%s", out)
	}
}
