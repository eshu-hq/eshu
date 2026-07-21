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
)

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
// requirement every materialized-edge family carries, computed directly from
// families rather than hand-maintained in the manifest YAML: "exhaustive
// coverage = both rows present" (#5351 design §1) is a structural rule of
// every family, not a per-family editorial choice, so encoding it in code
// means a 13th family added to reducer.MaterializedEdgeFamilies() can never
// drift out of sync with its own required-scenario-type pair the way a
// hand-authored YAML row could.
func materializedEdgeScenarioRequirements(families []string) []replaycoverage.ScenarioRequirement {
	out := make([]replaycoverage.ScenarioRequirement, 0, len(families))
	for _, family := range families {
		out = append(out, replaycoverage.ScenarioRequirement{
			Surface: MaterializedEdgeSurfacePrefix + family,
			ScenarioTypes: []replaycoverage.DepthScenarioType{
				replaycoverage.ScenarioTypeBaseline,
				replaycoverage.ScenarioTypeFault,
			},
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
	// Issue is the tracked child issue reference, e.g. "#5352".
	Issue string
	// Waived is the ISO-8601 (YYYY-MM-DD) date the waiver was recorded.
	Waived string
	// Reason is a short human explanation of why the family has no Odù yet.
	Reason string
}

// materializedEdgeWaiversBySurface indexes waivers by their surface key for
// O(1) lookup during finding rendering.
func materializedEdgeWaiversBySurface(waivers []MaterializedEdgeWaiver) map[string]MaterializedEdgeWaiver {
	out := make(map[string]MaterializedEdgeWaiver, len(waivers))
	for _, w := range waivers {
		out[w.Surface] = w
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
// family enumeration against Ifá's own materialized-edge coverage manifest,
// mirroring RunCoverage's shape (coverage.go) with one addition: waivers.
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

	waiverList := materializedEdgeWaiversBySurface(in.Waivers)
	familySet := make(map[string]struct{}, len(in.Families))
	for _, f := range in.Families {
		familySet[MaterializedEdgeSurfacePrefix+f] = struct{}{}
	}

	var danglingWaivers []string
	for surface := range waiverList {
		if _, ok := familySet[surface]; !ok {
			danglingWaivers = append(danglingWaivers, surface)
		}
	}
	sort.Strings(danglingWaivers)
	for _, surface := range danglingWaivers {
		gr.Add(goldengate.Finding{
			Phase: "manifest", Check: surface, OK: false, Required: in.Blocking,
			Detail: fmt.Sprintf("stale waiver: %q is not (or no longer) an enumerated materialized-edge family", surface),
		})
	}

	coveredSurfaces := make(map[string]struct{}, len(cov.Surfaces))
	for _, sc := range cov.Surfaces {
		if sc.Status == replaycoverage.StatusCovered {
			coveredSurfaces[sc.Surface.Key] = struct{}{}
		}
	}
	var staleWaivers []string
	for surface := range waiverList {
		if _, covered := coveredSurfaces[surface]; covered {
			staleWaivers = append(staleWaivers, surface)
		}
	}
	sort.Strings(staleWaivers)
	for _, surface := range staleWaivers {
		gr.Add(goldengate.Finding{
			Phase: "manifest", Check: surface, OK: false, Required: in.Blocking,
			Detail: fmt.Sprintf("stale waiver: %q now has real coverage; remove its waivers: row", surface),
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
func materializedEdgeFinding(sc replaycoverage.SurfaceCoverage, waivers map[string]MaterializedEdgeWaiver, blocking bool) goldengate.Finding {
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
	if w, waived := waivers[sc.Surface.Key]; waived {
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
