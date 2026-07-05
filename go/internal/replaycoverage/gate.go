// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Inputs are the loaded registries, manifest, and resolver the gate reconciles.
// The command loads these from disk; tests construct them in memory.
type Inputs struct {
	// Inventory is the generated surface inventory (implemented-lane collectors).
	Inventory capabilitycatalog.SurfaceInventory
	// FactKinds is the generated fact-kind registry (read surfaces).
	FactKinds []facts.FactKindRegistryEntry
	// Ledger is the parser-backing ledger (parsers).
	Ledger ParserLedger
	// LanguageLedger is the language-feature-parity ledger: the denominator for
	// the C-11 (#4364) language-parser scoreboard. It is visibility-only and does
	// not feed the blocking surface reconcile.
	LanguageLedger LanguageLedger
	// LanguageExemptions are the manifest's language-scoreboard exemptions
	// (corpus-exercised languages), keyed "language:<name>".
	LanguageExemptions []Exemption
	// Matrix is the capability matrix (claims).
	Matrix capabilitycatalog.Matrix
	// ProductClaims is the public product claim-to-proof ledger.
	ProductClaims capabilitycatalog.ProductClaimLedger
	// CLIShapes is the B-12 snapshot CLI query-shape catalog.
	CLIShapes map[string]goldengate.QueryShape
	// Authorization is the authorization catalog permission-family registry.
	Authorization capabilitycatalog.AuthorizationCatalog
	// AuthzProofs are the scoped-route proof scenarios used by authz entries.
	AuthzProofs AuthzProofLedger
	// Manifest is the curated coverage manifest.
	Manifest Manifest
	// DepthRequirements is the depth-requirement taxonomy (C-13 #4366): retractable
	// node types and the reducer drain. When its RetractableNodeTypes is empty the
	// gate skips depth derivation entirely (the pre-C-13 breadth-only behavior),
	// so callers that do not provide it keep the original semantics.
	DepthRequirements DepthRequirements
	// ProofGates is the CI-gate registry used to validate proof_gate names.
	ProofGates *cigates.Registry
	// Resolver verifies a manifest entry's scenario artifact exists.
	Resolver Resolver
	// Blocking flips every coverage finding from advisory to required. Local
	// exploratory runs can leave it false; CI sets it true now that the C-lane
	// coverage gaps have burned down.
	Blocking bool
}

// RunGate enumerates the supported surfaces, reconciles them against the manifest
// and resolver, builds the coverage-report artifact, and renders the findings as
// a goldengate report. The goldengate report carries the advisory→blocking
// semantics: in advisory mode it never fails on a coverage gap; in blocking mode
// it fails on any uncovered, unresolved, or stale surface.
func RunGate(in Inputs) (Coverage, CoverageReport, *goldengate.Report) {
	supported := EnumerateSupported(in.Inventory, in.FactKinds, in.Ledger, in.Matrix, in.ProductClaims, in.CLIShapes, in.Authorization)
	manifest := in.Manifest
	depthExempt := map[string]string{}
	// C-13/#4370: derive depth (scenario_type) requirements per applicable
	// surface. Gated on a non-empty depth spec so pre-C-13 callers keep
	// breadth-only semantics.
	if len(in.DepthRequirements.RetractableNodeTypes) > 0 || len(in.DepthRequirements.RetractableEdgeTypes) > 0 {
		supported = append(supported, EnumerateDepthSurfaces(in.DepthRequirements, in.FactKinds)...)
		sortSupportedSurfaces(supported)
		manifest.Requirements = unionRequirements(manifest.Requirements, DeriveRequirements(supported, in.DepthRequirements, in.FactKinds))
		depthExempt = depthExemptions(in.DepthRequirements)
	}
	surfaceManifest := manifestForSupportedReconcile(manifest, supported, in.LanguageLedger)
	cov := Reconcile(supported, surfaceManifest, in.Resolver)
	var proofGateDetails proofGateValidationDetails
	if in.ProofGates != nil {
		proofGateDetails = proofGateValidationDetailsByScenario(manifest, in.AuthzProofs, in.ProofGates)
		cov = applyProofGateValidation(cov, proofGateDetails)
	}
	cov = applyDepthExemptions(cov, depthExempt)
	rep := BuildReport(cov, in.Blocking)
	// The language-parser scoreboard is computed alongside the surface report but
	// kept out of the blocking findings below: C-11 (#4364) is visibility-only, so
	// the uncovered languages (the C-12 #4365 worklist) are listed without failing
	// the gate, and the single Blocking knob stays the only severity control.
	languageSurfaces := languageFixtureCoverageSurfaces(in.LanguageLedger, manifest, in.Resolver, proofGateDetails)
	scoreboardSurfaces := appendDistinctSurfaceCoverage(cov.Surfaces, languageSurfaces)
	rep.LanguageScoreboard = BuildLanguageScoreboard(in.LanguageLedger, in.LanguageExemptions, scoreboardSurfaces)
	gr := &goldengate.Report{}
	for _, f := range Findings(cov, in.Blocking) {
		gr.Add(f)
	}
	return cov, rep, gr
}

func languageFixtureCoverageSurfaces(
	ledger LanguageLedger,
	manifest Manifest,
	resolver Resolver,
	proofGateDetails proofGateValidationDetails,
) []SurfaceCoverage {
	if len(ledger.Languages) == 0 || len(manifest.Coverage) == 0 {
		return nil
	}
	ledgerLanguages := map[string]struct{}{}
	for _, lang := range ledger.Languages {
		ledgerLanguages[lang.Language] = struct{}{}
	}
	seen := map[string]struct{}{}
	languageManifest := Manifest{Version: manifest.Version}
	var supported []SupportedSurface
	for _, entry := range manifest.Coverage {
		if entry.Scenario != ScenarioParserFixture || entry.ScenarioType != ScenarioTypeBaseline {
			continue
		}
		name, ok := strings.CutPrefix(entry.Surface, ParserSurfacePrefix)
		if !ok || name == "" {
			continue
		}
		if _, ok := ledgerLanguages[name]; !ok {
			continue
		}
		if _, ok := seen[entry.Surface]; ok {
			continue
		}
		seen[entry.Surface] = struct{}{}
		languageManifest.Coverage = append(languageManifest.Coverage, entry)
		supported = append(supported, SupportedSurface{
			Registry: RegistryParserLedger,
			Key:      entry.Surface,
			Detail:   "language parser fixture",
		})
	}
	if len(supported) == 0 {
		return nil
	}
	cov := Reconcile(supported, languageManifest, resolver)
	for i := range cov.Surfaces {
		scenario := cov.Surfaces[i].Scenario
		if scenario == nil {
			continue
		}
		if detail, invalid := proofGateDetails.byProofGate[scenario.ProofGate]; invalid {
			cov.Surfaces[i].Status = StatusUnresolved
			cov.Surfaces[i].Detail = detail
		}
	}
	return cov.Surfaces
}

func manifestForSupportedReconcile(manifest Manifest, supported []SupportedSurface, languageLedger LanguageLedger) Manifest {
	if len(manifest.Coverage) == 0 {
		return manifest
	}
	supportedKeys := map[string]struct{}{}
	for _, surface := range supported {
		supportedKeys[surface.Key] = struct{}{}
	}
	languageNames := map[string]struct{}{}
	for _, lang := range languageLedger.Languages {
		languageNames[lang.Language] = struct{}{}
	}
	filtered := manifest
	filtered.Coverage = make([]CoverageEntry, 0, len(manifest.Coverage))
	for _, entry := range manifest.Coverage {
		if shouldDeferToLanguageScoreboard(entry, supportedKeys, languageNames) {
			continue
		}
		filtered.Coverage = append(filtered.Coverage, entry)
	}
	return filtered
}

func shouldDeferToLanguageScoreboard(entry CoverageEntry, supportedKeys map[string]struct{}, languageNames map[string]struct{}) bool {
	if entry.Scenario != ScenarioParserFixture || entry.ScenarioType != ScenarioTypeBaseline {
		return false
	}
	if _, ok := supportedKeys[entry.Surface]; ok {
		return false
	}
	name, ok := strings.CutPrefix(entry.Surface, ParserSurfacePrefix)
	if !ok || name == "" {
		return false
	}
	_, ok = languageNames[name]
	return ok
}

func appendDistinctSurfaceCoverage(base, extra []SurfaceCoverage) []SurfaceCoverage {
	out := append([]SurfaceCoverage(nil), base...)
	seen := map[string]struct{}{}
	for _, sc := range out {
		seen[surfaceCoverageKey(sc)] = struct{}{}
	}
	for _, sc := range extra {
		key := surfaceCoverageKey(sc)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sc)
	}
	return out
}

func surfaceCoverageKey(sc SurfaceCoverage) string {
	return sc.Surface.Key + "\x00" + string(surfaceCoverageScenarioType(sc))
}

func applyProofGateValidation(cov Coverage, details proofGateValidationDetails) Coverage {
	if len(details.byProofGate) == 0 && len(details.byAuthzRef) == 0 {
		return cov
	}
	usedAuthzRefs := map[string]struct{}{}
	for i := range cov.Surfaces {
		scenario := cov.Surfaces[i].Scenario
		if scenario == nil {
			continue
		}
		if detail, invalid := details.byAuthzRef[scenario.Ref]; invalid && scenario.Scenario == ScenarioAuthzScopedRoute {
			usedAuthzRefs[scenario.Ref] = struct{}{}
			cov.Surfaces[i].Status = StatusUnresolved
			cov.Surfaces[i].Detail = detail
			continue
		}
		if detail, invalid := details.byProofGate[scenario.ProofGate]; invalid {
			cov.Surfaces[i].Status = StatusUnresolved
			cov.Surfaces[i].Detail = detail
		}
	}
	var staleAuthzRefs []string
	for ref := range details.byAuthzRef {
		if _, used := usedAuthzRefs[ref]; !used {
			staleAuthzRefs = append(staleAuthzRefs, ref)
		}
	}
	sort.Strings(staleAuthzRefs)
	for _, ref := range staleAuthzRefs {
		cov.Surfaces = append(cov.Surfaces, SurfaceCoverage{
			Surface: SupportedSurface{
				Registry: RegistryAuthorizationCatalog,
				Key:      "authz_family:" + ref,
				Detail:   "stale authorization proof-ledger row",
			},
			ScenarioType: ScenarioTypeBaseline,
			Status:       StatusUnresolved,
			Detail:       details.byAuthzRef[ref],
		})
	}
	sort.Slice(cov.Surfaces, func(i, j int) bool {
		left, right := cov.Surfaces[i], cov.Surfaces[j]
		if left.Surface.Registry != right.Surface.Registry {
			return left.Surface.Registry < right.Surface.Registry
		}
		if left.Surface.Key != right.Surface.Key {
			return left.Surface.Key < right.Surface.Key
		}
		return left.ScenarioType < right.ScenarioType
	})
	return cov
}
