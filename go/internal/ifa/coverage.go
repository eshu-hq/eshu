// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

const (
	// RegistryFactKindCoverage is Ifá's own replaycoverage.Registry for the
	// fact_kind:* surface family (design §2/§3).
	RegistryFactKindCoverage replaycoverage.Registry = "ifa_fact_kind"
	// RegistryNarrowedCorrelation is Ifá's own replaycoverage.Registry for the
	// narrowed_correlation:* surface family.
	RegistryNarrowedCorrelation replaycoverage.Registry = "ifa_narrowed_correlation"

	// FactKindSurfacePrefix is the ifa-coverage-manifest surface key prefix for
	// a fact-kind-registry entry.
	FactKindSurfacePrefix = "fact_kind:"
	// NarrowedCorrelationSurfacePrefix is the ifa-coverage-manifest surface key
	// prefix for a B-12 evidence-narrowed required correlation.
	NarrowedCorrelationSurfacePrefix = "narrowed_correlation:"

	// ManifestFileName is Ifá's own coverage manifest inside specs/.
	ManifestFileName = "ifa-coverage-manifest.v1.yaml"
)

// EnumerateSurfaces flattens DerivedExpectations into the deterministic
// replaycoverage.SupportedSurface set Ifá's own coverage gate reconciles: one
// fact_kind:<kind> surface per derived KindExpectation, and one
// narrowed_correlation:<rc-id> surface per B-12 evidence-narrowed required
// correlation. The result is sorted by registry then key, matching
// replaycoverage.EnumerateSupported's own ordering contract.
func EnumerateSurfaces(exp DerivedExpectations) []replaycoverage.SupportedSurface {
	out := make([]replaycoverage.SupportedSurface, 0, len(exp.Kinds)+len(exp.NarrowedCorrelations))
	for _, ke := range exp.Kinds {
		out = append(out, replaycoverage.SupportedSurface{
			Registry: RegistryFactKindCoverage,
			Key:      FactKindSurfacePrefix + ke.Kind,
			Detail:   fmt.Sprintf("fact kind %q", ke.Kind),
		})
	}
	for _, rc := range exp.NarrowedCorrelations {
		out = append(out, replaycoverage.SupportedSurface{
			Registry: RegistryNarrowedCorrelation,
			Key:      NarrowedCorrelationSurfacePrefix + rc.ID,
			Detail:   fmt.Sprintf("evidence-narrowed required correlation %q (%s)", rc.ID, rc.Description),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Registry != out[j].Registry {
			return out[i].Registry < out[j].Registry
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// OduResolver implements replaycoverage.Resolver over the cataloged Odùs
// (design §3). Every entry it resolves must use the odu scenario kind; the
// RunCoverage guard rejects any other scenario before reconciliation runs.
type OduResolver struct {
	// Catalog indexes every cataloged Odù by name (Catalog()/CatalogByName()).
	Catalog map[string]Odu
	// Expectations is the derived expectation set a narrowed_correlation ref
	// resolves against.
	Expectations DerivedExpectations
	// Registry is the fact-kind registry, keyed by Kind, used to classify a
	// fact_kind ref as registry-only (blank PayloadSchema) or schema-backed.
	Registry map[string]facts.FactKindRegistryEntry
}

// Resolve implements replaycoverage.Resolver.
func (r OduResolver) Resolve(entry replaycoverage.CoverageEntry) (bool, string) {
	// An Ifá coverage entry is only meaningful when it binds a surface to an
	// Odù via the odu scenario. A valid-but-non-odu scenario (e.g. baseline)
	// must not resolve as covered even with a cataloged ref — otherwise the
	// nonOduScenarioGuard finding and a "covered" report row would contradict.
	if entry.Scenario != replaycoverage.ScenarioOdu {
		return false, fmt.Sprintf("ifa coverage entry uses scenario %q, want %q", entry.Scenario, replaycoverage.ScenarioOdu)
	}
	odu, ok := r.Catalog[entry.Ref]
	if !ok {
		return false, fmt.Sprintf("no cataloged Odù named %q", entry.Ref)
	}
	switch {
	case strings.HasPrefix(entry.Surface, FactKindSurfacePrefix):
		kind := strings.TrimPrefix(entry.Surface, FactKindSurfacePrefix)
		return r.resolveFactKind(kind, odu)
	case strings.HasPrefix(entry.Surface, NarrowedCorrelationSurfacePrefix):
		rcID := strings.TrimPrefix(entry.Surface, NarrowedCorrelationSurfacePrefix)
		return r.resolveNarrowedCorrelation(rcID, odu)
	default:
		return false, fmt.Sprintf("unrecognized ifa coverage surface %q", entry.Surface)
	}
}

// resolveFactKind implements the fact_kind:K rule (design §3): true iff odu
// carries at least one fact of kind K and ValidateOduPayloads passes for it.
func (r OduResolver) resolveFactKind(kind string, odu Odu) (bool, string) {
	present := false
	for _, envelope := range odu.Facts {
		if envelope.FactKind == kind {
			present = true
			break
		}
	}
	if !present {
		return false, fmt.Sprintf("odù %q carries no fact of kind %q", odu.Name, kind)
	}
	if err := ValidateOduPayloads(odu, r.Registry); err != nil {
		return false, err.Error()
	}
	if entry, ok := r.Registry[kind]; !ok || strings.TrimSpace(entry.PayloadSchema) == "" {
		return true, fmt.Sprintf("odù %q carries fact kind %q (registry-only derivation, no payload schema)", odu.Name, kind)
	}
	return true, fmt.Sprintf("odù %q carries fact kind %q and its payload validates against the fixturepack schema", odu.Name, kind)
}

// resolveNarrowedCorrelation implements the narrowed_correlation:rc-id rule
// (design §3): true iff odu names a cataloged fixture and EvidenceSatisfies
// reports the correlation's evidence-kind filter is met by odu's own
// production-extractor evidence.
func (r OduResolver) resolveNarrowedCorrelation(rcID string, odu Odu) (bool, string) {
	var rc *goldengate.RequiredCorrelation
	for i := range r.Expectations.NarrowedCorrelations {
		if r.Expectations.NarrowedCorrelations[i].ID == rcID {
			rc = &r.Expectations.NarrowedCorrelations[i]
			break
		}
	}
	if rc == nil {
		return false, fmt.Sprintf("no evidence-narrowed required correlation %q in the loaded B-12 snapshot", rcID)
	}
	ev := DiscoveredEvidence(odu)
	ok, detail := EvidenceSatisfies(*rc, ev)
	return ok, fmt.Sprintf("odù %q: %s", odu.Name, detail)
}

// CoverageInputs are the loaded inputs RunCoverage reconciles.
type CoverageInputs struct {
	// Expectations is the derived P1 expectation set (Derive's output).
	Expectations DerivedExpectations
	// Manifest is Ifá's own loaded coverage manifest.
	Manifest replaycoverage.Manifest
	// Catalog indexes every cataloged Odù by name.
	Catalog map[string]Odu
	// Registry is the fact-kind registry, keyed by Kind.
	Registry map[string]facts.FactKindRegistryEntry
	// ProofGates is the CI-gate registry used to validate proof_gate names. Nil
	// skips proof-gate validation (for tests that construct a manifest in
	// memory without a matching ci-gates registry).
	ProofGates *cigates.Registry
	// Blocking flips every coverage finding from advisory to required.
	Blocking bool
}

// RunCoverage reconciles Ifá's own derived surfaces against its own coverage
// manifest, mirroring go/internal/replaycoverage/gate.go's RunGate shape:
// reject any non-odu scenario in Ifá's own manifest (a P1-specific guard no
// other replaycoverage caller needs), reconcile, validate proof-gate names,
// build the report (dropping registries EnumerateSurfaces never populates),
// and render goldengate findings.
func RunCoverage(in CoverageInputs) (replaycoverage.Coverage, replaycoverage.CoverageReport, *goldengate.Report) {
	gr := &goldengate.Report{}

	if guardErrs := nonOduScenarioGuard(in.Manifest); len(guardErrs) > 0 {
		for _, detail := range guardErrs {
			gr.Add(goldengate.Finding{Phase: "manifest", Check: "odu_scenario_guard", OK: false, Required: true, Detail: detail})
		}
	}

	supported := EnumerateSurfaces(in.Expectations)
	resolver := OduResolver{Catalog: in.Catalog, Expectations: in.Expectations, Registry: in.Registry}
	cov := replaycoverage.Reconcile(supported, in.Manifest, resolver)

	if in.ProofGates != nil {
		for _, err := range replaycoverage.ValidateRequiredProofGates(in.Manifest, replaycoverage.AuthzProofLedger{}, in.ProofGates) {
			gr.Add(goldengate.Finding{Phase: "proof_gates", Check: "proof_gate_validation", OK: false, Required: true, Detail: err.Error()})
		}
	}

	rep := replaycoverage.BuildReport(cov, in.Blocking)
	rep.Summaries = dropZeroTotalSummaries(rep.Summaries)

	for _, f := range replaycoverage.Findings(cov, in.Blocking) {
		gr.Add(f)
	}
	return cov, rep, gr
}

// nonOduScenarioGuard rejects any coverage entry in Ifá's own manifest whose
// scenario is not ScenarioOdu. Ifá's manifest exists only to bind fact_kind/
// narrowed_correlation surfaces to cataloged Odùs; any other scenario kind
// there is a copy-paste error from the replay-coverage manifest, not a valid
// Ifá binding, so it is always a required (blocking-mode-independent) failure.
func nonOduScenarioGuard(m replaycoverage.Manifest) []string {
	var details []string
	for _, entry := range m.Coverage {
		if entry.Scenario != replaycoverage.ScenarioOdu {
			details = append(details, fmt.Sprintf(
				"ifa coverage manifest surface %q uses scenario %q; every Ifá coverage entry must use scenario %q",
				entry.Surface, entry.Scenario, replaycoverage.ScenarioOdu,
			))
		}
	}
	return details
}

// dropZeroTotalSummaries removes registry summaries with no enumerated
// surfaces. replaycoverage.BuildReport pre-seeds its own seven registries
// (surface_inventory, fact_kind_registry, ...) so its report shape stays
// stable even with zero surfaces in any of them; those never apply to Ifá's
// own ifa_fact_kind/ifa_narrowed_correlation registries, so they would only
// ever appear as noise in Ifá's report.
func dropZeroTotalSummaries(summaries []replaycoverage.RegistrySummary) []replaycoverage.RegistrySummary {
	out := make([]replaycoverage.RegistrySummary, 0, len(summaries))
	for _, s := range summaries {
		if s.Total == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}
