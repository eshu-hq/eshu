// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestMaterializedEdgeCoverageLockstepAgainstRealSpecs is the #5351 gate: it
// proves the committed specs/ifa-materialized-edge-coverage.v1.yaml is
// honest against reducer.MaterializedEdgeFamilies() and the real ci-gates
// registry, in BLOCKING mode. It is the "pure go test" local command the
// ifa-materialized-edge-coverage CI gate runs (specs/ci-gates.v1.yaml): every
// one of the 12 allProjectionDomains families must be either genuinely
// covered (both baseline and fault rows resolve) or carry a waiver naming a
// tracked issue — a family with neither must fail this test, exactly the
// drift the gate exists to catch.
func TestMaterializedEdgeCoverageLockstepAgainstRealSpecs(t *testing.T) {
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	manifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(materialized-edge manifest): %v", err)
	}
	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}
	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}

	families := reducer.MaterializedEdgeFamilies()
	if len(families) == 0 {
		t.Fatal("reducer.MaterializedEdgeFamilies() returned zero families; the registry itself is broken")
	}

	cov, gate, dangling := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families:   families,
		Manifest:   manifest,
		Waivers:    waivers,
		Catalog:    CatalogByName(),
		RepoRoot:   repoRoot,
		ProofGates: proofGates,
		Blocking:   true,
	})

	if len(cov.Stale) != 0 {
		t.Errorf("Stale = %v, want zero (every committed coverage row must name a real, currently-enumerated family)", cov.Stale)
	}
	if len(dangling) != 0 {
		t.Errorf("dangling waivers = %v, want zero (every waiver must name a real, currently-enumerated family)", dangling)
	}
	if gate.Failed() {
		t.Fatal("materialized-edge coverage gate failed in blocking mode: every family must be either covered (both scenario types) or waived with a tracked issue")
	}

	// sql_relationships is the one family with a genuinely-proven row as of
	// #5351: the BASELINE (ifa-determinism) is covered. The FAULT
	// (ifa-fault-injection) is NOT covered — it is a confirmed-false fault
	// (#5555) that is waived, not proven. Asserting them separately is the
	// honest-landing shape: baseline green, fault waived, at (surface × proof_gate)
	// granularity.
	baseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeBaseline)
	if baseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:sql_relationships (baseline) status = %q, detail=%q, want covered", baseline.Status, baseline.Detail)
	}
	fault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeFault)
	if fault.Status == replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:sql_relationships (fault) status = %q, want NOT covered (waived on #5555, not proven)", fault.Status)
	}

	// Every OTHER allProjectionDomains family must be waived on BOTH gates, not
	// silently dropped from the manifest (a (surface × proof_gate) row present in
	// neither coverage nor waivers is the exact drift this gate exists to catch —
	// proven not to slip past by gate.Failed() above, but assert the waiver keys
	// directly too so a future family added without either a coverage row or a
	// waiver fails loudly here). sql_relationships is asserted separately: its
	// baseline is covered (no waiver) and only its fault gate is waived.
	byKey := materializedEdgeWaiversByKey(waivers)
	for _, f := range families {
		if f == "sql_relationships" {
			if _, ok := byKey[materializedEdgeWaiverKey{Surface: MaterializedEdgeSurfacePrefix + f, ProofGate: materializedEdgeProofGateFault}]; !ok {
				t.Error("sql_relationships fault gate must carry a waiver (#5555)")
			}
			if _, ok := byKey[materializedEdgeWaiverKey{Surface: MaterializedEdgeSurfacePrefix + f, ProofGate: materializedEdgeProofGateBaseline}]; ok {
				t.Error("sql_relationships baseline is proven; it must NOT carry a waiver")
			}
			continue
		}
		for _, gate := range []string{materializedEdgeProofGateBaseline, materializedEdgeProofGateFault} {
			if _, ok := byKey[materializedEdgeWaiverKey{Surface: MaterializedEdgeSurfacePrefix + f, ProofGate: gate}]; !ok {
				t.Errorf("family %q gate %q has neither coverage nor a waiver", f, gate)
			}
		}
	}

	// Assert both proof gates this manifest references are CI-blocking with a
	// local command, mirroring coverage_lockstep_test.go's ifa-contract-layer
	// assertions: a non-blocking or command-less gate cannot be trusted to
	// keep a "covered" claim green.
	for _, gateID := range []string{"ifa-determinism", "ifa-fault-injection"} {
		var found *cigates.Gate
		for i := range proofGates.Gates {
			if proofGates.Gates[i].ID == gateID {
				found = &proofGates.Gates[i]
			}
		}
		if found == nil {
			t.Fatalf("%s gate not found in ci-gates registry", gateID)
		}
		if !found.Blocking {
			t.Errorf("%s must be CI-blocking", gateID)
		}
		if found.Local == nil || strings.TrimSpace(found.Local.Command) == "" {
			t.Errorf("%s gate has no local command", gateID)
		}
	}
}

func findMaterializedEdgeCoverage(t *testing.T, cov replaycoverage.Coverage, surface string, scenarioType replaycoverage.DepthScenarioType) replaycoverage.SurfaceCoverage {
	t.Helper()
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == surface && sc.ScenarioType == scenarioType {
			return sc
		}
	}
	t.Fatalf("no coverage row for surface %q scenario_type %q", surface, scenarioType)
	return replaycoverage.SurfaceCoverage{}
}
