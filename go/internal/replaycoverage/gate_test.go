// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func gateInputs(blocking bool) Inputs {
	return Inputs{
		Inventory: capabilitycatalog.SurfaceInventory{Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceCollector, Name: "aws", Readiness: capabilitycatalog.ReadinessImplemented},
		}},
		FactKinds: []facts.FactKindRegistryEntry{{Kind: "x", ReadSurface: "GET /api/v0/x"}},
		Ledger:    ParserLedger{Parsers: []ParserLedgerEntry{{Parser: "hcl"}}},
		Matrix:    capabilitycatalog.Matrix{},
		Manifest: Manifest{Coverage: []CoverageEntry{
			{Surface: "collector:aws", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "present"},
		}},
		Resolver: stubResolver{present: map[string]bool{"present": true}},
		Blocking: blocking,
	}
}

func TestRunGateAdvisoryDoesNotFail(t *testing.T) {
	// aws covered; read_surface and parser uncovered. Advisory => report not failed.
	_, rep, gr := RunGate(gateInputs(false))
	if gr.Failed() {
		t.Error("advisory gate must not fail even with uncovered surfaces")
	}
	if rep.Totals.Total != 3 || rep.Totals.Covered != 1 {
		t.Errorf("totals = %+v", rep.Totals)
	}
}

func TestRunGateBlockingFailsOnGap(t *testing.T) {
	_, _, gr := RunGate(gateInputs(true))
	if !gr.Failed() {
		t.Error("blocking gate must fail when surfaces are uncovered")
	}
}
