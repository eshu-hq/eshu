// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

func coverageFixtureRegistry() map[string]facts.FactKindRegistryEntry {
	return map[string]facts.FactKindRegistryEntry{
		"repository": {Kind: "repository", PayloadSchema: "sdk/go/factschema/schema/repository.v1.schema.json"},
	}
}

func coverageFixtureExpectations() DerivedExpectations {
	return DerivedExpectations{
		Kinds: []KindExpectation{
			{Kind: "repository", PayloadSchema: "sdk/go/factschema/schema/repository.v1.schema.json"},
			{Kind: "brand_new_kind"},
		},
		NarrowedCorrelations: []goldengate.RequiredCorrelation{rc29()},
	}
}

func coverageFixtureCatalog() map[string]Odu {
	return map[string]Odu{
		"odu:kustomize-deploys-from": kustomizeDeploysFromOdu().Odu,
		"odu:argocd-deploys-from":    argocdDeploysFromOdu().Odu,
	}
}

func TestRunCoverageUncoveredKindWithNoManifestRow(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:repository", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:kustomize-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+"brand_new_kind")
	if sc.Status != replaycoverage.StatusUncovered {
		t.Errorf("brand_new_kind status = %q, want uncovered", sc.Status)
	}
}

func TestRunCoverageCoveredFactKindResolves(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:repository", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:kustomize-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+"repository")
	if sc.Status != replaycoverage.StatusCovered {
		t.Errorf("repository status = %q, detail=%q, want covered", sc.Status, sc.Detail)
	}
}

// TestRunCoverageNonOduScenarioIsNotCovered proves a manifest row that uses a
// valid but non-odu scenario (here cassette) with an otherwise-resolvable
// surface and cataloged ref does NOT count as covered — the resolver rejects
// it, so the nonOduScenarioGuard finding cannot be contradicted by a "covered"
// report row (codex #4972 review).
func TestRunCoverageNonOduScenarioIsNotCovered(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:repository", Scenario: replaycoverage.ScenarioCassette, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:kustomize-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+"repository")
	if sc.Status == replaycoverage.StatusCovered {
		t.Errorf("repository status = %q, want NOT covered: a non-odu scenario binding must not count as covered", sc.Status)
	}
}

func TestRunCoverageRefToNonCatalogedOduIsUnresolved(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:repository", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:does-not-exist", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+"repository")
	if sc.Status != replaycoverage.StatusUnresolved {
		t.Errorf("repository status = %q, want unresolved for a ref naming no cataloged Odù", sc.Status)
	}
}

func TestRunCoverageOduThatDoesNotCarryKindIsUnresolved(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				// odu:argocd-deploys-from carries a repository fact too (same target),
				// so point at a kind it does NOT carry to force unresolved.
				{Surface: "fact_kind:brand_new_kind", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:argocd-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+"brand_new_kind")
	if sc.Status != replaycoverage.StatusUnresolved {
		t.Errorf("brand_new_kind status = %q, detail=%q, want unresolved (Odù does not carry that kind)", sc.Status, sc.Detail)
	}
}

func TestRunCoverageManifestRowForRemovedKindIsStale(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: DerivedExpectations{}, // no kinds/correlations enumerated at all
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:removed_kind", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:kustomize-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	found := false
	for _, s := range cov.Stale {
		if s == "fact_kind:removed_kind" {
			found = true
		}
	}
	if !found {
		t.Errorf("Stale = %v, want fact_kind:removed_kind (no enumerated surface matches it)", cov.Stale)
	}
}

func TestRunCoverageNarrowedCorrelationCoveredAndUnsatisfied(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "narrowed_correlation:rc-29", Scenario: replaycoverage.ScenarioOdu, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "odu:kustomize-deploys-from", ProofGate: "ifa-contract-layer"},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if sc.Status != replaycoverage.StatusCovered {
		t.Errorf("rc-29 (kustomize) status = %q, detail=%q, want covered", sc.Status, sc.Detail)
	}

	in.Manifest.Coverage[0].Ref = "odu:argocd-deploys-from"
	cov, _, _ = RunCoverage(in)
	sc = findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if sc.Status != replaycoverage.StatusUnresolved {
		t.Errorf("rc-29 (argocd) status = %q, want unresolved (wrong evidence kind)", sc.Status)
	}
}

func TestRunCoverageAdvisoryModeHasZeroRequiredFindings(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest:     replaycoverage.Manifest{},
		Blocking:     false,
	}
	_, _, gr := RunCoverage(in)
	for _, f := range gr.Findings {
		if f.Required {
			t.Errorf("advisory mode produced a required finding: %+v", f)
		}
	}
}

func TestRunCoverageReportDropsZeroTotalRegistrySummaries(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest:     replaycoverage.Manifest{},
	}
	_, rep, _ := RunCoverage(in)
	for _, s := range rep.Summaries {
		if s.Total == 0 {
			t.Errorf("report kept a zero-total registry summary: %+v", s)
		}
	}
}

func TestRunCoverageRejectsNonOduScenarioInIfaManifest(t *testing.T) {
	t.Parallel()

	in := CoverageInputs{
		Expectations: coverageFixtureExpectations(),
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{Surface: "fact_kind:repository", Scenario: replaycoverage.ScenarioCassette, ScenarioType: replaycoverage.ScenarioTypeBaseline, Ref: "testdata/cassettes/whatever.json", ProofGate: "ifa-contract-layer"},
			},
		},
		Blocking: false,
	}
	_, _, gr := RunCoverage(in)
	foundRequiredGuardFailure := false
	for _, f := range gr.Findings {
		if f.Required && !f.OK {
			foundRequiredGuardFailure = true
		}
	}
	if !foundRequiredGuardFailure {
		t.Fatal("expected a required, failing finding for a non-odu scenario in Ifá's own manifest, even in advisory mode")
	}
}

func findSurfaceCoverage(t *testing.T, cov replaycoverage.Coverage, key string) replaycoverage.SurfaceCoverage {
	t.Helper()
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == key {
			return sc
		}
	}
	t.Fatalf("no surface coverage found for key %q (surfaces: %+v)", key, cov.Surfaces)
	return replaycoverage.SurfaceCoverage{}
}
