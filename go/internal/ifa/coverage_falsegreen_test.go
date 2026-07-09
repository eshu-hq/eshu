// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// repoRootDir resolves the repository root relative to this test file, so the
// real B-12 golden snapshot can be loaded regardless of the working directory
// `go test` runs with (mirrors replaycoverage's own repoSpecsDir helper).
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func loadRealSnapshotRC29(t *testing.T) goldengate.RequiredCorrelation {
	t.Helper()
	snap, err := goldengate.LoadSnapshot(filepath.Join(repoRootDir(t), "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot(real B-12 snapshot): %v", err)
	}
	for _, rc := range snap.Graph.RequiredCorrelations {
		if rc.ID == "rc-29" {
			return rc
		}
	}
	t.Fatal("real B-12 snapshot has no required correlation rc-29 (drift: this test must track the committed snapshot)")
	return goldengate.RequiredCorrelation{}
}

// TestFalseGreenBaselineKustomizeSatisfiesRC29 proves the honest-green case
// FIRST (apirecording discipline, replay/apirecording_test.go:151-176): the
// real snapshot's rc-29, bound to odu:kustomize-deploys-from, must resolve
// covered before either deliberate break below is trusted to mean anything.
func TestFalseGreenBaselineKustomizeSatisfiesRC29(t *testing.T) {
	t.Parallel()

	rc29 := loadRealSnapshotRC29(t)
	in := CoverageInputs{
		Expectations: DerivedExpectations{NarrowedCorrelations: []goldengate.RequiredCorrelation{rc29}},
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{
					Surface:      NarrowedCorrelationSurfacePrefix + "rc-29",
					Scenario:     replaycoverage.ScenarioOdu,
					ScenarioType: replaycoverage.ScenarioTypeBaseline,
					Ref:          "odu:kustomize-deploys-from",
					ProofGate:    "ifa-contract-layer",
				},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if sc.Status != replaycoverage.StatusCovered {
		t.Fatalf("baseline rc-29/kustomize status = %q, detail=%q, want covered", sc.Status, sc.Detail)
	}
}

// TestFalseGreenWrongOduBreaksRC29 is the deliberate wrong-Odù break: binding
// rc-29 to the ArgoCD fixture instead of the Kustomize one must go red, because
// ArgoCD evidence carries ARGOCD_APPLICATION_SOURCE, not
// KUSTOMIZE_RESOURCE_REFERENCE. A coverage gate that stayed green here would be
// unable to tell the two deploy-source verbs apart — exactly the false green
// rc-29's evidence_kinds filter exists to prevent (see snapshot.go's
// RequiredCorrelation doc).
func TestFalseGreenWrongOduBreaksRC29(t *testing.T) {
	t.Parallel()

	// Baseline must already be proven green; re-run it inline so this test does
	// not depend on execution order between test functions.
	rc29 := loadRealSnapshotRC29(t)

	in := CoverageInputs{
		Expectations: DerivedExpectations{NarrowedCorrelations: []goldengate.RequiredCorrelation{rc29}},
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{
					Surface:      NarrowedCorrelationSurfacePrefix + "rc-29",
					Scenario:     replaycoverage.ScenarioOdu,
					ScenarioType: replaycoverage.ScenarioTypeBaseline,
					Ref:          "odu:argocd-deploys-from", // deliberate break: wrong Odù
					ProofGate:    "ifa-contract-layer",
				},
			},
		},
	}
	cov, _, _ := RunCoverage(in)
	sc := findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if sc.Status == replaycoverage.StatusCovered {
		t.Fatal("rc-29 bound to odu:argocd-deploys-from must not resolve covered (false green)")
	}
	if sc.Status != replaycoverage.StatusUnresolved {
		t.Errorf("rc-29/argocd status = %q, want unresolved", sc.Status)
	}
	if !strings.Contains(sc.Detail, "KUSTOMIZE_RESOURCE_REFERENCE") {
		t.Errorf("detail = %q, want it to name the missing KUSTOMIZE_RESOURCE_REFERENCE evidence kind", sc.Detail)
	}
}

// TestFalseGreenUnfilteredCorrelationBreaksCoverage is the deliberate
// unfiltered-rc break: binding the Kustomize Odù to rc-19 (the unfiltered,
// tool-agnostic DEPLOYS_FROM correlation with no evidence_kinds) instead of
// rc-29 must not silently pass. rc-19 is never one of Ifá's enumerated
// narrowed_correlation surfaces (design §1d: only rc's with evidence_kinds
// become ifa graph surfaces), so the manifest row naming it is stale drift,
// and the real rc-29 surface — which the manifest no longer binds — reports
// honestly uncovered.
func TestFalseGreenUnfilteredCorrelationBreaksCoverage(t *testing.T) {
	t.Parallel()

	rc29 := loadRealSnapshotRC29(t)

	in := CoverageInputs{
		Expectations: DerivedExpectations{NarrowedCorrelations: []goldengate.RequiredCorrelation{rc29}},
		Catalog:      coverageFixtureCatalog(),
		Registry:     coverageFixtureRegistry(),
		Manifest: replaycoverage.Manifest{
			Coverage: []replaycoverage.CoverageEntry{
				{
					Surface:      NarrowedCorrelationSurfacePrefix + "rc-19", // deliberate break: unfiltered rc, never enumerated
					Scenario:     replaycoverage.ScenarioOdu,
					ScenarioType: replaycoverage.ScenarioTypeBaseline,
					Ref:          "odu:kustomize-deploys-from",
					ProofGate:    "ifa-contract-layer",
				},
			},
		},
	}
	cov, _, _ := RunCoverage(in)

	staleFound := false
	for _, s := range cov.Stale {
		if s == NarrowedCorrelationSurfacePrefix+"rc-19" {
			staleFound = true
		}
	}
	if !staleFound {
		t.Errorf("Stale = %v, want narrowed_correlation:rc-19 (rc-19 has no evidence_kinds and is never an enumerable surface)", cov.Stale)
	}

	sc := findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if sc.Status != replaycoverage.StatusUncovered {
		t.Errorf("rc-29 status = %q, want uncovered (the manifest no longer binds it to anything)", sc.Status)
	}

	if staleFound && sc.Status == replaycoverage.StatusUncovered {
		// Both symptoms of the break must be visible: neither is individually
		// sufficient proof the gate would have caught this in isolation.
		return
	}
	t.Fatal("expected both the stale rc-19 row and the uncovered rc-29 row")
}

// TestFalseGreenEvidenceSatisfiesSanityInversion is the evidence.go sanity
// check underlying every break above: rc-29's evidence-kind filter must
// accept the Kustomize fixture and reject the ArgoCD one, or the false-green
// breaks above would be meaningless (both would fail, or neither would).
func TestFalseGreenEvidenceSatisfiesSanityInversion(t *testing.T) {
	t.Parallel()

	rc29 := loadRealSnapshotRC29(t)
	kustomizeEv := DiscoveredEvidence(kustomizeDeploysFromOdu().Odu)
	argocdEv := DiscoveredEvidence(argocdDeploysFromOdu().Odu)

	if ok, detail := EvidenceSatisfies(rc29, kustomizeEv); !ok {
		t.Fatalf("EvidenceSatisfies(rc-29, kustomize) = false, detail=%q, want true", detail)
	}
	if ok, _ := EvidenceSatisfies(rc29, argocdEv); ok {
		t.Fatal("EvidenceSatisfies(rc-29, argocd) = true, want false")
	}
}
