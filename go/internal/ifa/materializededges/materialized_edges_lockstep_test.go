// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/ifa"
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
//
// It reconciles the direct-materialization families (#6181) on the same terms.
// Reconciling only the shared half is what made the gate blind to them: an
// unenumerated family produces no row, no finding and no output, so the gate
// reported green without knowing it existed. Their coverage is tracked by
// #6228 and every one of them currently resolves through a waiver.
func TestMaterializedEdgeCoverageLockstepAgainstRealSpecs(t *testing.T) {
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	manifest, waivers, err := LoadMaterializedEdgeLedger(specsDir)
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeLedger: %v", err)
	}
	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}

	// Both halves of the reducer's materialized-edge surface, because the gate
	// is only as honest as the family list it reconciles against. Running it on
	// the shared half alone is exactly the blindness #6181 reported: the direct
	// families produce no row, no finding and no output, and the gate reports
	// green without ever knowing they exist.
	shared := reducer.MaterializedEdgeFamilies()
	if len(shared) == 0 {
		t.Fatal("reducer.MaterializedEdgeFamilies() returned zero families; the registry itself is broken")
	}
	direct := reducer.DirectMaterializedEdgeFamilies()
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; the registry itself is broken")
	}
	families := append(append([]string{}, shared...), direct...)
	sort.Strings(families)

	cov, gate, dangling := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families:   families,
		Manifest:   manifest,
		Waivers:    waivers,
		Catalog:    ifa.CatalogByName(),
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
		for _, finding := range gate.Findings {
			if finding.Required && !finding.OK {
				t.Errorf("materialized-edge gate finding %q failed: %s", finding.Check, finding.Detail)
			}
		}
		t.Error("materialized-edge coverage gate failed in blocking mode: every family must be either covered (both scenario types) or waived with a tracked issue")
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

	// #5998 promotes rationale_edges after both live matrices proved the same
	// production-shaped cassette and full-record expected set. Pin both rows and
	// the waiver deletion so the ledger cannot drift back to extractor-only
	// credit while the live wiring remains in place.
	rationaleBaseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"rationale_edges", replaycoverage.ScenarioTypeBaseline)
	if rationaleBaseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:rationale_edges (baseline) status = %q, detail=%q, want covered", rationaleBaseline.Status, rationaleBaseline.Detail)
	}
	rationaleFault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"rationale_edges", replaycoverage.ScenarioTypeFault)
	if rationaleFault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:rationale_edges (fault) status = %q, detail=%q, want covered", rationaleFault.Status, rationaleFault.Detail)
	}
	for _, waiver := range waivers {
		if waiver.Surface == MaterializedEdgeSurfacePrefix+"rationale_edges" {
			t.Errorf("stale rationale_edges waiver remains for proof gate %q; #5998 requires both waivers to be removed with the live rows", waiver.ProofGate)
		}
	}

	// #5999 promotes repo_dependency only after both live gates execute the
	// maintenance-backed cassette and its seven-edge oracle. Pin both resolved
	// rows and the waiver deletion so a dangling Odù ref reports the uncovered
	// status with the explicit no-waiver detail instead of disappearing behind
	// the gate's aggregate failure.
	repoDependencyBaseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"repo_dependency", replaycoverage.ScenarioTypeBaseline)
	if repoDependencyBaseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:repo_dependency (baseline) status = %q, detail=%q, want covered", repoDependencyBaseline.Status, repoDependencyBaseline.Detail)
	}
	repoDependencyFault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"repo_dependency", replaycoverage.ScenarioTypeFault)
	if repoDependencyFault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:repo_dependency (fault) status = %q, detail=%q, want covered", repoDependencyFault.Status, repoDependencyFault.Detail)
	}
	for _, waiver := range waivers {
		if waiver.Surface == MaterializedEdgeSurfacePrefix+"repo_dependency" {
			t.Errorf("stale repo_dependency waiver remains for proof gate %q; #5999 requires both waivers to be removed with the live rows", waiver.ProofGate)
		}
	}

	// #6003 promotes workload_dependency only when both live matrices resolve
	// their coverage rows and both waivers are gone. Keep the package contract
	// tied to that executable ledger so stale "remain waived" prose cannot
	// survive after the gate becomes blocking proof.
	workloadBaseline := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"workload_dependency", replaycoverage.ScenarioTypeBaseline)
	if workloadBaseline.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:workload_dependency (baseline) status = %q, detail=%q, want covered", workloadBaseline.Status, workloadBaseline.Detail)
	}
	workloadFault := findMaterializedEdgeCoverage(t, cov, MaterializedEdgeSurfacePrefix+"workload_dependency", replaycoverage.ScenarioTypeFault)
	if workloadFault.Status != replaycoverage.StatusCovered {
		t.Errorf("materialized_edges:workload_dependency (fault) status = %q, detail=%q, want covered", workloadFault.Status, workloadFault.Detail)
	}
	for _, waiver := range waivers {
		if waiver.Surface == MaterializedEdgeSurfacePrefix+"workload_dependency" {
			t.Errorf("stale workload_dependency waiver remains for proof gate %q; #6003 requires both waivers to be removed with the live rows", waiver.ProofGate)
		}
	}
	docBytes, err := os.ReadFile(filepath.Join(repoRoot, "go", "internal", "ifa", "materializededges", "doc.go"))
	if err != nil {
		t.Fatalf("ReadFile(materializededges/doc.go): %v", err)
	}
	docContract := string(docBytes)
	workloadDocStart := strings.Index(docContract, "// Workload dependencies")
	if workloadDocStart < 0 {
		t.Fatal("materializededges package contract has no workload_dependency paragraph")
	}
	workloadDocContract := docContract[workloadDocStart:]
	if !strings.Contains(workloadDocContract, "Both live matrices drive the committed cassette") {
		t.Error("materializededges package contract must say both live workload_dependency matrices drive the committed cassette")
	}
	if strings.Contains(workloadDocContract, "remain waived") {
		t.Error("materializededges package contract still says workload_dependency coverage remains waived")
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
			"go/internal/ifa/testdata/rationale/**",
			// The guards moved out of ifa (#6053), so the old per-file glob
			// matched nothing. This asserts the trigger that actually covers
			// them now -- dropping the dead glob without repointing here would
			// have left both live gates un-retriggered by a guard change.
			"go/internal/ifa/materializededges/**",
			"go/internal/reducer/code_call*.go",
			"go/internal/storage/cypher/*code_call*.go",
			"sdk/go/factschema/codegraph/v1/repository.go",
			"go/internal/ifa/documentation_family_odu.go",
			"go/internal/ifa/documentation_family_catalog.go",
			"go/internal/reducer/documentation_edge*.go",
			"go/internal/storage/cypher/*documentation*.go",
			"sdk/go/factschema/documentation/v1/**",
			"specs/ifa-materialized-edge-coverage.v1.yaml",
			"scripts/lib/ifa_rationale_live.sh",
			"testdata/cassettes/rationale/**",
		} {
			if !slices.Contains(found.Triggers, trigger) {
				t.Errorf("%s gate does not trigger on %q; a fixture, catalog, or vacuity-guard change could keep materialized-edge coverage green without rerunning its live proof", gateID, trigger)
			}
		}
		// deployable_unit_edges (#5993) triggers: its coverage rows have landed,
		// so this pin now keeps the ci-gates.v1.yaml trigger lists from drifting
		// away from the family's real source paths under normal maintenance.
		for _, trigger := range []string{
			"go/internal/reducer/deployable_unit*.go",
			"go/internal/storage/cypher/canonical_deployable_unit_edges.go",
			"go/internal/storage/cypher/edge_writer_rowmaps.go",
			"go/internal/ifa/deployable_unit_family_catalog.go",
			"go/internal/ifa/deployable_unit_family_odu.go",
			"testdata/cassettes/deployableunit/**",
			"go/internal/ifa/testdata/deployableunit/**",
			"go/cmd/bootstrap-index/**",
		} {
			if !slices.Contains(found.Triggers, trigger) {
				t.Errorf("%s gate does not trigger on %q; a catalog or vacuity-guard change could keep deployable_unit_edges covered without rerunning its live proof", gateID, trigger)
			}
		}
	}
}

// materializedEdgeFamilyTriggerStems maps every materialized-edge family to
// the substring its own gate triggers carry. It is a hand-maintained map on
// purpose: a family's trigger paths follow no derivable rule (code_calls owns
// `code_call*`, sql_relationships owns `sql_relationship*`, and neither is the
// family key), so guessing the stem from the family name would produce a
// check that silently matches nothing.
//
// A WRONG stem is loud -- the family's own coverage row goes red the moment it
// lands. A MISSING entry is silent, which is why
// TestEveryCoveredFamilyTriggersBothLiveGates asserts this map's key set
// equals reducer.MaterializedEdgeFamilies() in both directions, mirroring
// TestMaterializedEdgeIdentityByFamilyIsTotal.
//
// Stems for the not-yet-covered families are prospective: nothing checks them
// until that family gets a coverage row, which is exactly the point --
// declaring them now means the family that lands coverage inherits a working
// check instead of writing one under time pressure.
var materializedEdgeFamilyTriggerStems = map[string]string{
	"code_calls":                 "code_call",
	"codeowners_ownership_edges": "codeowners",
	"deployable_unit_edges":      "deployable_unit",
	"documentation_edges":        "documentation",
	// handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997) are the
	// one exception to "stem is a prefix of the family's own name": all three
	// share ONE cassette and ONE handler-side extraction seam, so their real
	// ci-gates.v1.yaml triggers are wired under the shared "symbol_runtime"
	// name (e.g. "go/internal/ifa/symbol_runtime_family_odu.go",
	// "testdata/cassettes/symbolruntime/**"), not under any of the three
	// family names individually. A stem of "handles_route" (etc.) matched
	// nothing in either gate block and went unnoticed while these rows were
	// still waived -- exactly the WRONG-STEM-goes-loud-only-once-covered case
	// this map's own doc comment above describes; it went red the moment
	// their coverage rows landed.
	"handles_route":        "symbol_runtime",
	"inheritance_edges":    "inheritance",
	"invokes_cloud_action": "symbol_runtime",
	"rationale_edges":      "rationale",
	"repo_dependency":      "repo_dependency",
	"runs_in":              "symbol_runtime",
	"shell_exec":           "shell_exec",
	"sql_relationships":    "sql_relationship",
	"submodule_pin_edges":  "submodule",
	"workload_dependency":  "workload_dependency",
}

// TestEveryCoveredFamilyTriggersBothLiveGates closes the third side of the
// triangle. Two checks already exist: the registry-derived loop in
// scripts/test-verify-ifa-determinism.sh proves registry ⊆ workflow, and the
// selector table proves the concrete paths select both gates. Neither notices
// a family that declares NO triggers at all -- both iterate what the registry
// already names, so an unwired family is not merely unchecked, it is
// vacuously green.
//
// A coverage row asserts the live matrices proved that family. A family with
// no trigger in a gate never re-runs that gate when its own Odù, cassette,
// extractor, or writer changes, so the row keeps asserting a proof that has
// gone stale. This check binds the two.
//
// It is keyed to COVERAGE ROWS, not to all families, and deliberately so.
// Requiring triggers of every family would land a red row for every family
// that is honestly waived and not yet wired, and a check that ships red is a
// check somebody switches off. Keyed to coverage it lands clean and stays purely
// prospective: the next family to claim a row has to wire its triggers in the
// same change.
//
// What it does NOT cover: whether the triggers a covered family declares are
// the RIGHT ones. Nothing checks that, and nothing plausibly can -- a gate
// cannot know which files feed a family's proof. That remains a review
// responsibility.
func TestEveryCoveredFamilyTriggersBothLiveGates(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	families := reducer.MaterializedEdgeFamilies()
	for _, family := range families {
		// Blank is rejected alongside missing. `"new_family": ""` is the natural
		// placeholder and it satisfies key-set equality, so checking presence
		// alone would let the map stay total while the value it contributes
		// matches every trigger vacuously -- the exact silence this half of the
		// guard exists to break.
		if stem, ok := materializedEdgeFamilyTriggerStems[family]; !ok || strings.TrimSpace(stem) == "" {
			t.Errorf("family %q has no usable trigger stem (present=%t, stem=%q); give it a non-blank entry in materializedEdgeFamilyTriggerStems or this family's coverage row would be checked against nothing", family, ok, stem)
		}
	}
	for family := range materializedEdgeFamilyTriggerStems {
		if !slices.Contains(families, family) {
			t.Errorf("trigger stem declared for %q, which reducer.MaterializedEdgeFamilies() does not enumerate; remove the stale entry", family)
		}
	}

	manifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(materialized-edge manifest): %v", err)
	}
	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}
	triggersByGate := map[string][]string{}
	for _, gateID := range []string{materializedEdgeProofGateBaseline, materializedEdgeProofGateFault} {
		var found bool
		for i := range proofGates.Gates {
			if proofGates.Gates[i].ID == gateID {
				triggersByGate[gateID] = proofGates.Gates[i].Triggers
				found = true
			}
		}
		if !found {
			t.Fatalf("%s gate not found in ci-gates registry", gateID)
		}
	}

	covered := map[string]struct{}{}
	for _, row := range manifest.Coverage {
		covered[strings.TrimPrefix(row.Surface, MaterializedEdgeSurfacePrefix)] = struct{}{}
	}
	if len(covered) == 0 {
		t.Fatal("no materialized-edge family carries a coverage row; this check would pass vacuously")
	}

	for _, family := range families {
		if _, ok := covered[family]; !ok {
			continue
		}
		stem := strings.TrimSpace(materializedEdgeFamilyTriggerStems[family])
		if stem == "" {
			// Missing or blank, both already reported by the totality loop above.
			// Continuing here would otherwise match every trigger and report the
			// family as wired.
			continue
		}
		for gateID, triggers := range triggersByGate {
			matched := 0
			for _, trigger := range triggers {
				if strings.Contains(trigger, stem) {
					matched++
				}
			}
			if matched == 0 {
				t.Errorf("family %q claims a coverage row but the %s gate declares no trigger containing %q; that gate never re-runs when the family's own inputs change, so the row asserts a proof nobody refreshes", family, gateID, stem)
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
