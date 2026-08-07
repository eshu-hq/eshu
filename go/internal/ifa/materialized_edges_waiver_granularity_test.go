// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestMaterializedEdgeWaiverRequiresProofGate proves the granularity contract
// at load time: the waiver key is (surface, proof_gate), so a waiver written at
// the old per-family granularity — a surface with no proof_gate — must fail to
// load, not silently soften both the baseline and fault rows of that family.
func TestMaterializedEdgeWaiverRequiresProofGate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	content := `version: "1"
waivers:
  - surface: "materialized_edges:code_calls"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "per-family granularity, no proof_gate"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	if _, err := LoadMaterializedEdgeWaivers(path); err == nil {
		t.Fatal("a waiver with no proof_gate (per-family granularity) must fail to load: the waiver key is (surface, proof_gate)")
	}
}

// TestMaterializedEdgeWaiverRejectsUnknownProofGate proves a waiver may only
// name one of the two proof gates a required scenario-type pair maps to; an
// arbitrary gate string must fail loudly rather than waive a row no gate proves.
func TestMaterializedEdgeWaiverRejectsUnknownProofGate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	content := `version: "1"
waivers:
  - surface: "materialized_edges:code_calls"
    proof_gate: "not-a-real-gate"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "bogus gate"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	if _, err := LoadMaterializedEdgeWaivers(path); err == nil {
		t.Fatal("a waiver naming an unknown proof_gate must fail to load")
	}
}

// TestMaterializedEdgeWaiverRejectsDuplicateKeyNotSurface proves uniqueness is
// enforced at (surface, proof_gate) granularity: the SAME surface may carry a
// baseline waiver AND a fault waiver (distinct keys), but two waivers with the
// same (surface, proof_gate) are a duplicate and must fail.
func TestMaterializedEdgeWaiverRejectsDuplicateKeyNotSurface(t *testing.T) {
	t.Parallel()

	// Same surface, two DIFFERENT proof gates: allowed.
	okPath := filepath.Join(t.TempDir(), "ok.yaml")
	okContent := `version: "1"
waivers:
  - surface: "materialized_edges:code_calls"
    proof_gate: "ifa-determinism"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "baseline waiver"
  - surface: "materialized_edges:code_calls"
    proof_gate: "ifa-fault-injection"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "fault waiver"
`
	if err := os.WriteFile(okPath, []byte(okContent), 0o600); err != nil {
		t.Fatalf("write ok manifest: %v", err)
	}
	if _, err := LoadMaterializedEdgeWaivers(okPath); err != nil {
		t.Fatalf("same surface with two distinct proof gates must load: %v", err)
	}

	// Same surface, same proof gate twice: duplicate key.
	dupPath := filepath.Join(t.TempDir(), "dup.yaml")
	dupContent := `version: "1"
waivers:
  - surface: "materialized_edges:code_calls"
    proof_gate: "ifa-determinism"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "first"
  - surface: "materialized_edges:code_calls"
    proof_gate: "ifa-determinism"
    issue: "#5543"
    waived: "2026-07-20"
    reason: "duplicate"
`
	if err := os.WriteFile(dupPath, []byte(dupContent), 0o600); err != nil {
		t.Fatalf("write dup manifest: %v", err)
	}
	if _, err := LoadMaterializedEdgeWaivers(dupPath); err == nil {
		t.Fatal("two waivers with the same (surface, proof_gate) must fail as a duplicate")
	}
}

// TestMaterializedEdgeFaultWaiverDoesNotGreenBaseline is the core granularity
// falsegreen: a family with NO coverage that carries only a fault-injection
// waiver must still fail the blocking gate on its unwaived baseline row. A
// per-family waiver would have greened both; a per-(surface, proof_gate) waiver
// greens only the fault row.
func TestMaterializedEdgeFaultWaiverDoesNotGreenBaseline(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	_, gate, _ := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families: reducer.MaterializedEdgeFamilies(),
		Manifest: replaycoverage.Manifest{},
		Waivers: []MaterializedEdgeWaiver{{
			Surface: MaterializedEdgeSurfacePrefix + "code_calls", ProofGate: materializedEdgeProofGateFault,
			Issue: "#5543", Waived: "2026-07-20", Reason: "fault only",
		}},
		Catalog:  CatalogByName(),
		RepoRoot: repoRoot,
		Blocking: true,
	})

	if !gate.Failed() {
		t.Fatal("a fault-only waiver must leave the baseline row an unwaived required failure")
	}
	baseline := findFinding(t, gate, MaterializedEdgeSurfacePrefix+"code_calls")
	if baseline.OK {
		t.Fatal("fault-only waiver greened the code_calls baseline row: waiver granularity is not per-(surface, proof_gate)")
	}
	if !baseline.Required {
		t.Error("the unwaived code_calls baseline row must be a required (blocking) failure")
	}
	fault := findFinding(t, gate, MaterializedEdgeSurfacePrefix+"code_calls|fault")
	if !fault.OK {
		t.Errorf("the fault row carries a matching waiver and must be softened to advisory OK, got %+v", fault)
	}
}

// TestMaterializedEdgeBaselineWaiverDoesNotGreenFault is the inverse: a
// baseline-only waiver must leave the fault row unwaived.
func TestMaterializedEdgeBaselineWaiverDoesNotGreenFault(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	_, gate, _ := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families: reducer.MaterializedEdgeFamilies(),
		Manifest: replaycoverage.Manifest{},
		Waivers: []MaterializedEdgeWaiver{{
			Surface: MaterializedEdgeSurfacePrefix + "code_calls", ProofGate: materializedEdgeProofGateBaseline,
			Issue: "#5543", Waived: "2026-07-20", Reason: "baseline only",
		}},
		Catalog:  CatalogByName(),
		RepoRoot: repoRoot,
		Blocking: true,
	})

	baseline := findFinding(t, gate, MaterializedEdgeSurfacePrefix+"code_calls")
	if !baseline.OK {
		t.Errorf("the baseline row carries a matching waiver and must be softened to advisory OK, got %+v", baseline)
	}
	fault := findFinding(t, gate, MaterializedEdgeSurfacePrefix+"code_calls|fault")
	if fault.OK {
		t.Fatal("baseline-only waiver greened the code_calls fault row: waiver granularity is not per-(surface, proof_gate)")
	}
}

// TestMaterializedEdgeSQLFaultHonestlyWaived proves the #5351 honest landing
// shape against the REAL committed manifest and waivers: the SQL baseline row
// is genuinely covered (green), the SQL fault row is WAIVED (advisory, not
// covered) and its finding prints the tracked defect issue #5555.
func TestMaterializedEdgeSQLFaultHonestlyWaived(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	manifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}

	cov, gate, _ := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families: reducer.MaterializedEdgeFamilies(),
		Manifest: manifest,
		Waivers:  waivers,
		Catalog:  CatalogByName(),
		RepoRoot: repoRoot,
		Blocking: true,
	})
	if gate.Failed() {
		t.Fatalf("real manifest must keep the gate green: %+v", gate.Findings)
	}

	// Baseline: genuinely covered, not merely waived.
	baseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeBaseline)
	if baseline.Status != replaycoverage.StatusCovered {
		t.Errorf("sql_relationships baseline status = %q, want covered", baseline.Status)
	}

	// Fault: NOT covered — it must be uncovered (no coverage row) and softened
	// by the waiver, never a resolved "covered" claim.
	fault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"sql_relationships", replaycoverage.ScenarioTypeFault)
	if fault.Status == replaycoverage.StatusCovered {
		t.Error("sql_relationships fault must NOT be covered; the confirmed-false fault (#5555) is waived, not proven")
	}

	faultFinding := findFinding(t, gate, MaterializedEdgeSurfacePrefix+"sql_relationships|fault")
	if !faultFinding.OK || faultFinding.Required {
		t.Errorf("waived sql fault row must be advisory OK, got %+v", faultFinding)
	}
	if !strings.Contains(faultFinding.Detail, "#5555") {
		t.Errorf("waived sql fault finding must print the tracked issue #5555, got detail %q", faultFinding.Detail)
	}
}

// findFinding returns the goldengate finding whose Check equals check.
func findFinding(t *testing.T, gate *goldengate.Report, check string) goldengate.Finding {
	t.Helper()
	for _, f := range gate.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("no finding with check %q in %+v", check, gate.Findings)
	return goldengate.Finding{}
}
