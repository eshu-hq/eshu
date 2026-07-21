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

	// The one family with real coverage as of #5351.
	for _, scenarioType := range []replaycoverage.DepthScenarioType{replaycoverage.ScenarioTypeBaseline, replaycoverage.ScenarioTypeFault} {
		sc := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", scenarioType)
		if sc.Status != replaycoverage.StatusCovered {
			t.Errorf("materialized_edges:sql_relationships (%s) status = %q, detail=%q, want covered", scenarioType, sc.Status, sc.Detail)
		}
	}

	// Every OTHER allProjectionDomains family must be waived, not silently
	// dropped from the manifest entirely (a family present in neither
	// coverage nor waivers is the exact drift class this gate exists to
	// catch — proven not to slip past by gate.Failed() above, but assert the
	// waiver count directly too so a future family added without EITHER a
	// coverage row or a waiver fails loudly here, not just via a generic
	// "gate failed" message).
	waivedCount := 0
	for _, f := range families {
		if f == "sql_relationships" {
			continue
		}
		if _, ok := materializedEdgeWaiversBySurface(waivers)[MaterializedEdgeSurfacePrefix+f]; !ok {
			t.Errorf("family %q has neither coverage nor a waiver", f)
			continue
		}
		waivedCount++
	}
	if want := len(families) - 1; waivedCount != want {
		t.Errorf("waived family count = %d, want %d (every allProjectionDomains family except sql_relationships)", waivedCount, want)
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
