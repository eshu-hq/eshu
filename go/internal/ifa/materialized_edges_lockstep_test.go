// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"slices"
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
// one of the 14 allProjectionDomains families must be either genuinely
// covered (baseline and fault rows resolve) or carry a waiver naming a tracked
// issue. SQL relationships additionally requires a live delta row.
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
	// Every (family, proof_gate) pair must be EITHER covered or waived, and never
	// both. This is derived from the manifest rather than special-casing family
	// names: #5543 proves families one at a time, so a rule written as
	// "sql_relationships is covered, everything else is waived" needs editing on
	// every single one of those PRs — and a rule you must edit to make a change
	// pass is a rule that stops being read.
	//
	// The "never both" half is the stale-waiver rule: a waiver surviving next to
	// real coverage keeps advertising a gap that is closed.
	byKey := materializedEdgeWaiversByKey(waivers)
	covered := map[materializedEdgeWaiverKey]struct{}{}
	for _, sc := range manifest.Coverage {
		covered[materializedEdgeWaiverKey{Surface: sc.Surface, ProofGate: sc.ProofGate}] = struct{}{}
	}
	for _, f := range families {
		for _, gate := range []string{materializedEdgeProofGateBaseline, materializedEdgeProofGateFault} {
			key := materializedEdgeWaiverKey{Surface: MaterializedEdgeSurfacePrefix + f, ProofGate: gate}
			_, isCovered := covered[key]
			_, isWaived := byKey[key]
			switch {
			case !isCovered && !isWaived:
				t.Errorf("family %q gate %q has neither coverage nor a waiver", f, gate)
			case isCovered && isWaived:
				t.Errorf("family %q gate %q is both covered and waived; remove the waiver in the change that adds the coverage row", f, gate)
			}
		}
	}

	if gate.Failed() {
		t.Fatal("materialized-edge coverage gate failed in blocking mode: every family must be either covered (both scenario types) or waived with a tracked issue")
	}

	// sql_relationships has genuinely-proven BASELINE, DELTA, and (#5555)
	// FAULT rows under the ifa-determinism / ifa-fault-injection matrices.
	baseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeBaseline)
	if baseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:sql_relationships (baseline) status = %q, detail=%q, want covered", baseline.Status, baseline.Detail)
	}
	delta := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeDeltaTombstone)
	if delta.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:sql_relationships (delta_tombstone) status = %q, detail=%q, want covered", delta.Status, delta.Detail)
	}
	// The fault dimension is covered as of #5974. It was waived for months on the
	// belief that cell_failgraphwrite_sql did not fire in CI; it did, and the
	// assertion reading its marker was calling a binary the runner lacks.
	fault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeFault)
	if fault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:sql_relationships (fault) status = %q, detail=%q, want covered", fault.Status, fault.Detail)
	}

	// code_calls is the first non-SQL family promoted from extractor-only proof
	// to both live matrices (#5991). Pin both rows and the waiver deletion here:
	// a manifest row without a catalog/resolver guard cannot resolve covered,
	// while a surviving waiver next to real coverage is stale by definition.
	codeCallsBaseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"code_calls", replaycoverage.ScenarioTypeBaseline)
	if codeCallsBaseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:code_calls (baseline) status = %q, detail=%q, want covered", codeCallsBaseline.Status, codeCallsBaseline.Detail)
	}
	codeCallsFault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"code_calls", replaycoverage.ScenarioTypeFault)
	if codeCallsFault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:code_calls (fault) status = %q, detail=%q, want covered", codeCallsFault.Status, codeCallsFault.Detail)
	}
	for _, waiver := range waivers {
		if waiver.Surface == MaterializedEdgeSurfacePrefix+"code_calls" {
			t.Errorf("stale code_calls waiver remains for proof gate %q; #5991 requires both waivers to be removed with the live rows", waiver.ProofGate)
		}
	}

	// documentation_edges is the second non-SQL family promoted to both live
	// matrices (#5994). Its history is the reason this block exists: commit
	// 42e188ba1 seeded a coverage row on the EXTRACTOR proof alone and had to
	// withdraw it, because neither gate executed the family. Pin both rows and
	// the waiver deletion so that cannot recur silently.
	docsBaseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"documentation_edges", replaycoverage.ScenarioTypeBaseline)
	if docsBaseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:documentation_edges (baseline) status = %q, detail=%q, want covered", docsBaseline.Status, docsBaseline.Detail)
	}
	docsFault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"documentation_edges", replaycoverage.ScenarioTypeFault)
	if docsFault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:documentation_edges (fault) status = %q, detail=%q, want covered", docsFault.Status, docsFault.Detail)
	}
	for _, waiver := range waivers {
		if waiver.Surface == MaterializedEdgeSurfacePrefix+"documentation_edges" {
			t.Errorf("stale documentation_edges waiver remains for proof gate %q; #5994 requires both waivers to be removed with the live rows", waiver.ProofGate)
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
		for _, trigger := range []string{
			"go/internal/ifa/catalog_seed.go",
			"go/internal/ifa/code_call_family_catalog.go",
			"go/internal/ifa/materialized_edges*.go",
			"go/internal/reducer/code_call*.go",
			"go/internal/storage/cypher/*code_call*.go",
			"sdk/go/factschema/codegraph/v1/repository.go",
			"go/internal/ifa/documentation_family_odu.go",
			"go/internal/ifa/documentation_family_catalog.go",
			"go/internal/reducer/documentation_edge*.go",
			"go/internal/storage/cypher/*documentation*.go",
			"sdk/go/factschema/documentation/v1/**",
		} {
			if !slices.Contains(found.Triggers, trigger) {
				t.Errorf("%s gate does not trigger on %q; a catalog or vacuity-guard change could keep a family covered without rerunning its live proof", gateID, trigger)
			}
		}
	}
}

func TestMaterializedEdgeScenarioRequirementsIncludeSQLDeltaLiveOnly(t *testing.T) {
	t.Parallel()

	requirements := materializedEdgeScenarioRequirements([]string{"code_calls", "sql_relationships"})
	if len(requirements) != 2 {
		t.Fatalf("requirements = %d, want 2", len(requirements))
	}
	for _, requirement := range requirements {
		hasDelta := false
		for _, scenarioType := range requirement.ScenarioTypes {
			if scenarioType == replaycoverage.ScenarioTypeDeltaTombstone {
				hasDelta = true
			}
		}
		wantDelta := requirement.Surface == MaterializedEdgeSurfacePrefix+"sql_relationships"
		if hasDelta != wantDelta {
			t.Errorf("surface %q has delta requirement = %v, want %v", requirement.Surface, hasDelta, wantDelta)
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
