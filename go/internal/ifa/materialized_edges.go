// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

const (
	// RegistryMaterializedEdges is Ifá's own replaycoverage.Registry for the
	// materialized_edges:<domain> surface family (#5351): it binds an Odù
	// expectation to a reducer-materialized graph edge family, so a reducer
	// materialization silently ceasing to produce an edge family is caught,
	// mirroring RegistryFactKindCoverage/RegistryNarrowedCorrelation
	// (coverage.go) but for graph-write exhaustiveness rather than
	// payload-schema or evidence-narrowed-correlation coverage.
	RegistryMaterializedEdges replaycoverage.Registry = "ifa_materialized_edges"

	// MaterializedEdgeSurfacePrefix is the ifa-materialized-edge-coverage-manifest
	// surface key prefix for one reducer.MaterializedEdgeFamilies() entry.
	MaterializedEdgeSurfacePrefix = "materialized_edges:"

	// MaterializedEdgeManifestFileName is Ifá's own materialized-edge coverage
	// manifest inside specs/, sibling to ManifestFileName
	// (ifa-coverage-manifest.v1.yaml).
	MaterializedEdgeManifestFileName = "ifa-materialized-edge-coverage.v1.yaml"

	// materializedEdgeProofGateBaseline and materializedEdgeProofGateFault are
	// the two CI proof gates the exhaustiveness contract pairs with each
	// family's required scenario types: the P2 determinism matrix proves the
	// baseline row (and SQL's delta row), while the P4 fault-injection matrix
	// proves the fault row. Waivers remain keyed by (surface, proof_gate) because
	// only the baseline/fault dimensions may be waived; SQL delta-live is a
	// required, unwaived claim.
	materializedEdgeProofGateBaseline = "ifa-determinism"
	materializedEdgeProofGateFault    = "ifa-fault-injection"
)

// validMaterializedEdgeWaiverProofGates is the closed set of proof gates a
// waiver may name: exactly the two gates the required baseline/fault
// scenario-type pair maps to. A waiver naming anything else would waive a row
// no gate proves, so LoadMaterializedEdgeWaivers rejects it.
var validMaterializedEdgeWaiverProofGates = map[string]struct{}{
	materializedEdgeProofGateBaseline: {},
	materializedEdgeProofGateFault:    {},
}

// materializedEdgeWaiverProofGateFor maps only waivable baseline/fault scenarios to
// their waiver proof-gate key. Delta-live is deliberately unwaivable even
// though ifa-determinism proves it, so delta_tombstone returns "": it can
// neither consume a baseline waiver nor make that distinct waiver look stale.
// Unknown scenario types also return "" and therefore fail closed.
func materializedEdgeWaiverProofGateFor(scenarioType replaycoverage.DepthScenarioType) string {
	switch scenarioType {
	case replaycoverage.ScenarioTypeBaseline:
		return materializedEdgeProofGateBaseline
	case replaycoverage.ScenarioTypeFault:
		return materializedEdgeProofGateFault
	default:
		return ""
	}
}

// materializedEdgeWaiverKey is the (surface, proof_gate) identity a waiver is
// matched on — equal to the reconciled row key, so a fault-injection waiver can
// never revoke credit for a proven determinism (baseline) row and vice versa.
type materializedEdgeWaiverKey struct {
	Surface   string
	ProofGate string
}

// EnumerateMaterializedEdgeSurfaces flattens the drift-proof family list
// (reducer.MaterializedEdgeFamilies()) into the deterministic
// replaycoverage.SupportedSurface set the materialized-edge coverage gate
// reconciles: one materialized_edges:<family> surface per family. families is
// expected pre-sorted (reducer.MaterializedEdgeFamilies() sorts its own
// output), so the result inherits that order without a second sort.
func EnumerateMaterializedEdgeSurfaces(families []string) []replaycoverage.SupportedSurface {
	out := make([]replaycoverage.SupportedSurface, 0, len(families))
	for _, family := range families {
		out = append(out, replaycoverage.SupportedSurface{
			Registry: RegistryMaterializedEdges,
			Key:      MaterializedEdgeSurfacePrefix + family,
			Detail:   fmt.Sprintf("reducer-materialized edge family %q", family),
		})
	}
	return out
}

// materializedEdgeScenarioRequirements builds the [baseline, fault]
// requirement every materialized-edge family carries, plus SQL's live delta
// requirement (#5554). The requirements are computed here rather than trusted
// from hand-maintained manifest YAML, so the structural coverage contract
// cannot silently drift.
func materializedEdgeScenarioRequirements(families []string) []replaycoverage.ScenarioRequirement {
	out := make([]replaycoverage.ScenarioRequirement, 0, len(families))
	for _, family := range families {
		scenarioTypes := []replaycoverage.DepthScenarioType{
			replaycoverage.ScenarioTypeBaseline,
			replaycoverage.ScenarioTypeFault,
		}
		if family == "sql_relationships" {
			scenarioTypes = []replaycoverage.DepthScenarioType{
				replaycoverage.ScenarioTypeBaseline,
				replaycoverage.ScenarioTypeDeltaTombstone,
				replaycoverage.ScenarioTypeFault,
			}
		}
		out = append(out, replaycoverage.ScenarioRequirement{
			Surface:       MaterializedEdgeSurfacePrefix + family,
			ScenarioTypes: scenarioTypes,
		})
	}
	return out
}

// MaterializedEdgeOduResolver implements replaycoverage.Resolver for
// materialized_edges:<family> surfaces (#5351). Every entry it resolves must
// use the odu scenario; resolution then dispatches to the family's own
// vacuity guard (resolveSQLRelationshipMaterializedEdges for
// "sql_relationships" today). A family with no registered vacuity guard
// cannot resolve covered even if a manifest row names one — this is
// deliberate: "add a domain = DATA ONLY" (design §3) covers the fixture and
// manifest rows, but a NEW family's first coverage always adds its own small
// vacuity-guard function too, mirroring the SQL family's.
type MaterializedEdgeOduResolver struct {
	// Catalog indexes every cataloged Odù by name (Catalog()/CatalogByName()).
	Catalog map[string]Odu
	// RepoRoot is the repository root expected-edge-set fixture paths resolve
	// against.
	RepoRoot string
}

// Resolve implements replaycoverage.Resolver.
func (r MaterializedEdgeOduResolver) Resolve(entry replaycoverage.CoverageEntry) (bool, string) {
	if entry.Scenario != replaycoverage.ScenarioOdu {
		return false, fmt.Sprintf("materialized-edge coverage entry uses scenario %q, want %q", entry.Scenario, replaycoverage.ScenarioOdu)
	}
	odu, ok := r.Catalog[entry.Ref]
	if !ok {
		return false, fmt.Sprintf("no cataloged Odù named %q", entry.Ref)
	}
	family := strings.TrimPrefix(entry.Surface, MaterializedEdgeSurfacePrefix)
	switch family {
	case "sql_relationships":
		if entry.ScenarioType == replaycoverage.ScenarioTypeDeltaTombstone {
			baseline, exists := r.Catalog[sqlFamilyOduName]
			if !exists {
				return false, fmt.Sprintf("no cataloged baseline Odù named %q", sqlFamilyOduName)
			}
			return resolveSQLRelationshipDeltaMaterializedEdges(
				baseline,
				odu,
				sqlFamilyDeltaLiveExpectedEdgesPath(r.RepoRoot),
			)
		}
		return resolveSQLRelationshipMaterializedEdges(odu, sqlFamilyExpectedEdgesPath(r.RepoRoot))
	default:
		return false, fmt.Sprintf("no vacuity guard registered for materialized-edge family %q", family)
	}
}

// MaterializedEdgeWaiver records that a materialized-edge family is
// deliberately left RED (no Odù coverage yet) with a tracked child issue,
// mirroring the C-13/C-14/F-6 gates' waiver pattern. A waiver softens an
// otherwise-required uncovered finding into an advisory one that still names
// the issue in every report — it never silently hides the gap.
type MaterializedEdgeWaiver struct {
	// Surface is the materialized_edges:<family> key the waiver applies to.
	Surface string
	// ProofGate is the CI proof gate this waiver softens for the surface, one of
	// materializedEdgeProofGateBaseline / materializedEdgeProofGateFault. It is
	// what makes a waiver per-(surface, proof_gate): waiving the fault gate does
	// not touch the surface's baseline row.
	ProofGate string
	// Issue is the tracked child issue reference, e.g. "#5352".
	Issue string
	// Waived is the ISO-8601 (YYYY-MM-DD) date the waiver was recorded.
	Waived string
	// Reason is a short human explanation of why the family's proof_gate row has
	// no Odù coverage yet.
	Reason string
}

// materializedEdgeWaiversByKey indexes waivers by their (surface, proof_gate)
// key for O(1) lookup during finding rendering.
func materializedEdgeWaiversByKey(waivers []MaterializedEdgeWaiver) map[materializedEdgeWaiverKey]MaterializedEdgeWaiver {
	out := make(map[materializedEdgeWaiverKey]MaterializedEdgeWaiver, len(waivers))
	for _, w := range waivers {
		out[materializedEdgeWaiverKey{Surface: w.Surface, ProofGate: w.ProofGate}] = w
	}
	return out
}

// MaterializedEdgeCoverageInputs are the loaded inputs
// RunMaterializedEdgeCoverage reconciles.
type MaterializedEdgeCoverageInputs struct {
	// Families is reducer.MaterializedEdgeFamilies()'s output.
	Families []string
	// Manifest is Ifá's own loaded materialized-edge coverage manifest
	// (coverage rows only; Requirements is computed from Families, not read
	// from the manifest file — see materializedEdgeScenarioRequirements).
	Manifest replaycoverage.Manifest
	// Waivers are the loaded child-issue waivers for families with no Odù yet.
	Waivers []MaterializedEdgeWaiver
	// Catalog indexes every cataloged Odù by name.
	Catalog map[string]Odu
	// RepoRoot is the repository root expected-edge-set fixture paths resolve
	// against.
	RepoRoot string
	// ProofGates is the CI-gate registry used to validate proof_gate names. Nil
	// skips proof-gate validation.
	ProofGates *cigates.Registry
	// Blocking flips every non-waived coverage finding from advisory to
	// required.
	Blocking bool
}

// RunMaterializedEdgeCoverage reconciles the drift-proof materialized-edge
// family enumeration against Ifá's own materialized-edge coverage manifest.
//
// The manifest is a CLAIMS LEDGER, not a roadmap: the unit of a claim is one
// (surface × scenario_type) row bound to a proof gate. This function never
// infers coverage for a dimension the ledger is silent about, and a required-
// but-unclaimed dimension fails the blocking gate. Baseline/fault waivers are
// per-(surface, proof_gate), while SQL delta-live is required and unwaived.
//
// It mirrors RunCoverage's shape (coverage.go) with one addition: waivers.
// Reconcile/BuildReport/Findings are reused wholesale from replaycoverage;
// only the final finding render is bespoke, because "a waived family is
// advisory but must still name its issue" has no equivalent in
// replaycoverage's generic exemption/depth-advisory vocabulary (an
// Exemption there needs no issue and cannot coexist with a scenario
// requirement — see replaycoverage.LoadManifest's declared-twice guard).
func RunMaterializedEdgeCoverage(in MaterializedEdgeCoverageInputs) (replaycoverage.Coverage, *goldengate.Report, []string) {
	gr := &goldengate.Report{}

	manifest := in.Manifest
	manifest.Requirements = materializedEdgeScenarioRequirements(in.Families)

	supported := EnumerateMaterializedEdgeSurfaces(in.Families)
	resolver := MaterializedEdgeOduResolver{Catalog: in.Catalog, RepoRoot: in.RepoRoot}
	cov := replaycoverage.Reconcile(supported, manifest, resolver)

	if in.ProofGates != nil {
		for _, err := range replaycoverage.ValidateRequiredProofGates(manifest, replaycoverage.AuthzProofLedger{}, in.ProofGates) {
			gr.Add(goldengate.Finding{Phase: "proof_gates", Check: "proof_gate_validation", OK: false, Required: true, Detail: err.Error()})
		}
	}

	waiverList := materializedEdgeWaiversByKey(in.Waivers)
	familySet := make(map[string]struct{}, len(in.Families))
	for _, f := range in.Families {
		familySet[MaterializedEdgeSurfacePrefix+f] = struct{}{}
	}

	// A waiver whose surface is not an enumerated family is dangling. Dedupe by
	// surface so a family's baseline+fault waivers do not report twice.
	danglingSet := make(map[string]struct{}, len(waiverList))
	for key := range waiverList {
		if _, ok := familySet[key.Surface]; !ok {
			danglingSet[key.Surface] = struct{}{}
		}
	}
	danglingWaivers := make([]string, 0, len(danglingSet))
	for surface := range danglingSet {
		danglingWaivers = append(danglingWaivers, surface)
	}
	sort.Strings(danglingWaivers)
	for _, surface := range danglingWaivers {
		gr.Add(goldengate.Finding{
			Phase: "manifest", Check: surface, OK: false, Required: in.Blocking,
			Detail: fmt.Sprintf("stale waiver: %q is not (or no longer) an enumerated materialized-edge family", surface),
		})
	}

	// A waiver for a (surface, proof_gate) row that is now genuinely covered is
	// stale and must be removed — checked at row granularity so a still-honest
	// fault waiver survives even after the baseline row of the same surface
	// gains real coverage (the SQL family's exact shape).
	coveredKeys := make(map[materializedEdgeWaiverKey]struct{}, len(cov.Surfaces))
	for _, sc := range cov.Surfaces {
		if sc.Status == replaycoverage.StatusCovered {
			coveredKeys[materializedEdgeWaiverKey{Surface: sc.Surface.Key, ProofGate: materializedEdgeWaiverProofGateFor(sc.ScenarioType)}] = struct{}{}
		}
	}
	var staleWaivers []string
	for key := range waiverList {
		if _, covered := coveredKeys[key]; covered {
			staleWaivers = append(staleWaivers, materializedEdgeWaiverDisplayKey(key))
		}
	}
	sort.Strings(staleWaivers)
	for _, display := range staleWaivers {
		gr.Add(goldengate.Finding{
			Phase: "manifest", Check: display, OK: false, Required: in.Blocking,
			Detail: fmt.Sprintf("stale waiver: %q now has real coverage; remove its waivers: row", display),
		})
	}

	for _, sc := range cov.Surfaces {
		gr.Add(materializedEdgeFinding(sc, waiverList, in.Blocking))
	}

	for _, surface := range cov.Stale {
		gr.Add(goldengate.Finding{
			Phase: "manifest", Check: surface, OK: false, Required: in.Blocking,
			Detail: "stale: manifest entry maps no supported materialized-edge family",
		})
	}

	return cov, gr, danglingWaivers
}

// materializedEdgeFinding renders one SurfaceCoverage row as a goldengate
// Finding, softening an uncovered/unresolved row with a matching waiver into
// an advisory (never-required) OK=true finding that still names the waiver's
// issue and reason. A stale-waiver-vs-covered-family conflict (a waiver left
// in place after real coverage landed) is reported separately by
// RunMaterializedEdgeCoverage comparing cov to in.Waivers before this
// function runs, not here — this function only renders the coverage side.
func materializedEdgeFinding(sc replaycoverage.SurfaceCoverage, waivers map[materializedEdgeWaiverKey]MaterializedEdgeWaiver, blocking bool) goldengate.Finding {
	ok := sc.Status == replaycoverage.StatusCovered || sc.Status == replaycoverage.StatusExempt
	check := sc.Surface.Key
	if sc.ScenarioType != "" && sc.ScenarioType != replaycoverage.ScenarioTypeBaseline {
		check = fmt.Sprintf("%s|%s", sc.Surface.Key, sc.ScenarioType)
	}
	if ok {
		return goldengate.Finding{
			Phase: string(sc.Surface.Registry), Check: check, OK: true, Required: false,
			Detail: fmt.Sprintf("%s: %s", sc.Status, sc.Detail),
		}
	}
	key := materializedEdgeWaiverKey{Surface: sc.Surface.Key, ProofGate: materializedEdgeWaiverProofGateFor(sc.ScenarioType)}
	if w, waived := waivers[key]; waived {
		return goldengate.Finding{
			Phase: string(sc.Surface.Registry), Check: check, OK: true, Required: false,
			Detail: fmt.Sprintf("waived (tracked in %s, %s): %s", w.Issue, w.Waived, w.Reason),
		}
	}
	return goldengate.Finding{
		Phase: string(sc.Surface.Registry), Check: check, OK: false, Required: blocking,
		Detail: fmt.Sprintf("%s: %s (no waiver on file)", sc.Status, sc.Detail),
	}
}

// materializedEdgeWaiverDisplayKey renders a waiver key for a finding message:
// bare surface for the baseline gate (so it matches the baseline row's own
// display key), surface|proof_gate otherwise.
func materializedEdgeWaiverDisplayKey(key materializedEdgeWaiverKey) string {
	if key.ProofGate == materializedEdgeProofGateBaseline {
		return key.Surface
	}
	return fmt.Sprintf("%s|%s", key.Surface, key.ProofGate)
}
