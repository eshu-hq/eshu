// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// offProofGate returns a registry row that is switched off the way an owner
// order does it: tier manual and non-blocking, with the reason recorded.
func offProofGate(id string) cigates.Gate {
	gate := replayProofGate(id, "bash scripts/verify-"+id+".sh", id+".yml")
	gate.Tier = cigates.TierManual
	gate.Blocking = false
	gate.CIOnlyReason = "disabled by owner order"
	return gate
}

// proofOffInputs reconciles one collector surface whose scenario names proofGate,
// so each case isolates how the gate's registry row changes that one surface.
func proofOffInputs(proofGate string, gates ...cigates.Gate) Inputs {
	return Inputs{
		Inventory: capabilitycatalog.SurfaceInventory{Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceCollector, Name: "aws", Readiness: capabilitycatalog.ReadinessImplemented},
		}},
		Manifest: Manifest{Coverage: []CoverageEntry{{
			Surface:      "collector:aws",
			Scenario:     ScenarioCassette,
			ScenarioType: ScenarioTypeBaseline,
			Ref:          "present",
			ProofGate:    proofGate,
		}}},
		Resolver:   stubResolver{present: map[string]bool{"present": true}},
		ProofGates: replayProofRegistry(gates...),
		Blocking:   true,
	}
}

func proofOffManifest(proofGate string) Manifest {
	return proofOffInputs(proofGate).Manifest
}

// Case 1: a proof gate the owner switched off leaves its surfaces unproven and
// reported. It must not fail the registry check.
func TestValidateRequiredProofGatesAllowsOwnerDisabledGate(t *testing.T) {
	registry := replayProofRegistry(offProofGate("replay-tier"))
	if errs := ValidateRequiredProofGates(proofOffManifest("replay-tier"), AuthzProofLedger{}, registry); len(errs) != 0 {
		t.Fatalf("owner-disabled proof gate must not fail validation: %v", errs)
	}
}

// Case 1 boundary: only a manual, non-blocking row is owner-disabled. A
// non-blocking row on a live tier is an advisory gate and still fails.
func TestValidateRequiredProofGatesStillRejectsNonBlockingGateOnALiveTier(t *testing.T) {
	gate := offProofGate("replay-tier")
	gate.Tier = cigates.TierPrePR
	errs := ValidateRequiredProofGates(proofOffManifest("replay-tier"), AuthzProofLedger{}, replayProofRegistry(gate))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `proof_gate "replay-tier" is not blocking`) {
		t.Fatalf("errors = %v, want one not-blocking error", errs)
	}
}

// Case 1 boundary: a disabled gate with no local command is still a broken row.
func TestValidateRequiredProofGatesStillChecksADisabledGateLocalCommand(t *testing.T) {
	gate := offProofGate("replay-tier")
	gate.Local = nil
	errs := ValidateRequiredProofGates(proofOffManifest("replay-tier"), AuthzProofLedger{}, replayProofRegistry(gate))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "has no local command") {
		t.Fatalf("errors = %v, want one missing-local-command error", errs)
	}
}

func TestRunGateReportsSurfaceWithDisabledProofGateAsProofOff(t *testing.T) {
	cov, rep, gr := RunGate(proofOffInputs("replay-tier", offProofGate("replay-tier")))
	if gr.Failed() {
		t.Fatalf("a disabled proof gate must be reported, not failed: %+v", gr)
	}
	if len(cov.Surfaces) != 1 || cov.Surfaces[0].Status != StatusProofOff {
		t.Fatalf("surfaces = %+v, want one proof_off surface", cov.Surfaces)
	}
	if !strings.Contains(cov.Surfaces[0].Detail, "replay-tier") {
		t.Errorf("detail = %q, want it to name the disabled gate", cov.Surfaces[0].Detail)
	}
	if rep.Totals.Total != 1 || rep.Totals.Covered != 0 || rep.Totals.ProofOff != 1 {
		t.Fatalf("totals = %+v, want total 1, covered 0, proof_off 1", rep.Totals)
	}
	if rep.Totals.PercentSatisfied != 0 {
		t.Errorf("percent satisfied = %v, want 0: an unproven surface is not satisfied", rep.Totals.PercentSatisfied)
	}
	if len(rep.Gaps) != 0 {
		t.Errorf("gaps = %v, want none: proof_off is reported in its own list", rep.Gaps)
	}
	if len(rep.ProofOff) != 1 || rep.ProofOff[0] != "collector:aws" {
		t.Errorf("proof_off = %v, want [collector:aws]", rep.ProofOff)
	}
}

// Case 2: a proof gate that is missing from the registry (a typo) still fails,
// whether the gate list is empty or holds other rows.
func TestRunGateStillFailsOnUnknownProofGate(t *testing.T) {
	in := proofOffInputs("typo-gate", offProofGate("replay-tier"))
	cov, rep, gr := RunGate(in)
	if !gr.Failed() {
		t.Fatal("an unknown proof gate must fail the blocking gate")
	}
	if cov.Surfaces[0].Status != StatusUnresolved || rep.Totals.ProofOff != 0 {
		t.Fatalf("status = %v, proof_off = %d, want unresolved and 0", cov.Surfaces[0].Status, rep.Totals.ProofOff)
	}
	errs := ValidateRequiredProofGates(in.Manifest, AuthzProofLedger{}, in.ProofGates)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `unknown proof_gate "typo-gate"`) {
		t.Fatalf("errors = %v, want one unknown proof_gate error", errs)
	}
}

// Case 3: an enabled blocking proof gate still covers the surface.
func TestRunGateCoversSurfaceWithEnabledBlockingProofGate(t *testing.T) {
	gate := replayProofGate("golden-corpus-gate", "bash scripts/verify-golden-corpus-gate.sh", "golden-corpus-gate.yml")
	cov, rep, gr := RunGate(proofOffInputs("golden-corpus-gate", gate))
	if gr.Failed() {
		t.Fatalf("an enabled blocking proof gate must pass: %+v", gr)
	}
	if cov.Surfaces[0].Status != StatusCovered || rep.Totals.Covered != 1 || rep.Totals.ProofOff != 0 {
		t.Fatalf("status = %v totals = %+v, want covered", cov.Surfaces[0].Status, rep.Totals)
	}
	if len(rep.ProofOff) != 0 {
		t.Errorf("proof_off = %v, want none", rep.ProofOff)
	}
}

func TestRenderDashboardNamesSurfacesWhoseProofGateIsOff(t *testing.T) {
	_, rep, _ := RunGate(proofOffInputs("replay-tier", offProofGate("replay-tier")))
	out := string(RenderDashboard(rep))
	for _, want := range []string{
		"**Overall: 0/1 surfaces satisfied (0.00%)**",
		"1 surface(s) are unproven because their proof gate is off",
		"`collector:aws`",
		"`replay-tier`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard is missing %q:\n%s", want, out)
		}
	}
}
