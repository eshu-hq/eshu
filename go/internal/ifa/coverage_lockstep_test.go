// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestCoverageLockstepAgainstRealSpecs proves the committed
// specs/ifa-coverage-manifest.v1.yaml is honest against the real fact-kind
// registry, the real B-12 snapshot, the real replay-coverage manifest, and the
// real ci-gates registry: it may leave surfaces honestly uncovered (that is
// the C-lane worklist, not a failure), but it must never claim an unresolved
// or stale binding, never reference an invalid proof_gate, and it must prove
// narrowed_correlation:rc-29 covered — the one B-12 correlation this milestone
// seeds a genuinely green Odù for.
func TestCoverageLockstepAgainstRealSpecs(t *testing.T) {
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	registryEntries := facts.FactKindRegistry()
	byKind := make(map[string]facts.FactKindRegistryEntry, len(registryEntries))
	for _, entry := range registryEntries {
		byKind[entry.Kind] = entry
	}

	snap, err := goldengate.LoadSnapshot(filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot(real): %v", err)
	}

	replayManifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, replaycoverage.ManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(replay-coverage-manifest): %v", err)
	}

	ifaManifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, ManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(ifa-coverage-manifest): %v", err)
	}

	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}

	exp, err := Derive(registryEntries, snap, replayManifest)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	in := CoverageInputs{
		Expectations: exp,
		Manifest:     ifaManifest,
		Catalog:      CatalogByName(),
		Registry:     byKind,
		ProofGates:   proofGates,
		Blocking:     false,
	}
	cov, _, _ := RunCoverage(in)

	if len(cov.Stale) != 0 {
		t.Errorf("Stale = %v, want zero (every committed row must name a real, currently-enumerated surface)", cov.Stale)
	}

	unresolved := 0
	for _, sc := range cov.Surfaces {
		if sc.Status == replaycoverage.StatusUnresolved {
			unresolved++
			t.Errorf("unresolved surface %s (%s): %s", sc.Surface.Key, sc.ScenarioType, sc.Detail)
		}
	}
	if unresolved != 0 {
		t.Fatalf("unresolved surfaces = %d, want zero", unresolved)
	}

	// ValidateRequiredProofGates requires every referenced proof_gate to be
	// CI-blocking (replaycoverage/proof_gates.go's validateProofGate), because a
	// non-blocking gate cannot be trusted to keep a "covered" claim green. The
	// ifa-contract-layer gate is deliberately kept advisory for P1 (the
	// blocking flip is P4, per the #4394 design), so every row in this
	// manifest necessarily produces exactly that one "is not blocking" error
	// and no other: the reference is otherwise well-formed (a real, known gate
	// with a local command and a CI workflow). Asserting the error shape here
	// — rather than asserting zero errors — still catches a typo'd gate id or a
	// gate missing its local command, without contradicting the P1 decision to
	// keep ifa-contract-layer non-blocking.
	for _, err := range replaycoverage.ValidateRequiredProofGates(ifaManifest, replaycoverage.AuthzProofLedger{}, proofGates) {
		msg := err.Error()
		if !strings.Contains(msg, `proof_gate "ifa-contract-layer" is not blocking`) {
			t.Errorf("unexpected proof-gate validation error (want only the known P1 advisory-gate error): %v", err)
		}
	}

	// validateProofGate returns the "is not blocking" error BEFORE it reaches
	// the missing-command / missing-workflow checks, so the assertion above
	// cannot, on its own, catch a gate that is both advisory AND missing its
	// local command or CI workflow. Assert well-formedness directly against the
	// registry so that compound breakage still fails here (#4959 tracks the
	// shared precedence gap in replaycoverage/proof_gates.go).
	var ifaGate *cigates.Gate
	for i := range proofGates.Gates {
		if proofGates.Gates[i].ID == "ifa-contract-layer" {
			ifaGate = &proofGates.Gates[i]
		}
	}
	if ifaGate == nil {
		t.Fatal("ifa-contract-layer gate not found in ci-gates registry")
	}
	if ifaGate.Local == nil || strings.TrimSpace(ifaGate.Local.Command) == "" {
		t.Error("ifa-contract-layer gate has no local command; the advisory proof-gate assertion above would mask this")
	}
	if strings.TrimSpace(ifaGate.CI.Workflow) == "" {
		t.Error("ifa-contract-layer gate has no CI workflow; the advisory proof-gate assertion above would mask this")
	}

	rc29Coverage := findSurfaceCoverage(t, cov, NarrowedCorrelationSurfacePrefix+"rc-29")
	if rc29Coverage.Status != replaycoverage.StatusCovered {
		t.Errorf("narrowed_correlation:rc-29 status = %q, detail=%q, want covered", rc29Coverage.Status, rc29Coverage.Detail)
	}

	// The five odu:demo-org-roundtrip rows (#4804) must resolve covered: the
	// cataloged Odù carries a fact of each gcp_* kind and every one validates
	// against the fixturepack schema the registry names.
	demoOrgKinds := []string{
		"gcp_cloud_resource",
		"gcp_cloud_relationship",
		"gcp_collection_warning",
		"gcp_dns_record",
		"gcp_iam_policy_observation",
	}
	for _, kind := range demoOrgKinds {
		sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+kind)
		if sc.Status != replaycoverage.StatusCovered {
			t.Errorf("fact_kind:%s status = %q, detail=%q, want covered", kind, sc.Status, sc.Detail)
		}
	}

	// Unlike the ifa-contract-layer rows above, golden-corpus-gate is blocking
	// with a local command and a CI workflow (specs/ci-gates.v1.yaml), so the
	// five odu:demo-org-roundtrip rows that name it must produce zero
	// proof-gate validation errors, not the advisory "is not blocking" error.
	for _, err := range replaycoverage.ValidateRequiredProofGates(ifaManifest, replaycoverage.AuthzProofLedger{}, proofGates) {
		if strings.Contains(err.Error(), `"golden-corpus-gate"`) {
			t.Errorf("unexpected golden-corpus-gate proof-gate validation error (want zero): %v", err)
		}
	}

	// Deliberate wrong-Odù false-green break (mirrors
	// coverage_falsegreen_test.go's rc-29/argocd break): binding
	// fact_kind:gcp_cloud_resource to odu:aws-pack — a cataloged Odù that
	// carries no gcp_cloud_resource fact at all — must not resolve covered. A
	// coverage gate that stayed green here could not tell a real GCP-family
	// binding apart from an unrelated cataloged Odù.
	wrongCoverage := make([]replaycoverage.CoverageEntry, len(ifaManifest.Coverage))
	copy(wrongCoverage, ifaManifest.Coverage)
	for i, entry := range wrongCoverage {
		if entry.Surface == FactKindSurfacePrefix+"gcp_cloud_resource" {
			wrongCoverage[i].Ref = "odu:aws-pack"
		}
	}
	wrongOduManifest := ifaManifest
	wrongOduManifest.Coverage = wrongCoverage

	wrongCov, _, _ := RunCoverage(CoverageInputs{
		Expectations: exp,
		Manifest:     wrongOduManifest,
		Catalog:      CatalogByName(),
		Registry:     byKind,
		ProofGates:   proofGates,
		Blocking:     false,
	})
	wrongSC := findSurfaceCoverage(t, wrongCov, FactKindSurfacePrefix+"gcp_cloud_resource")
	if wrongSC.Status == replaycoverage.StatusCovered {
		t.Fatal("fact_kind:gcp_cloud_resource bound to odu:aws-pack must not resolve covered (false green): odu:aws-pack carries no gcp_cloud_resource fact")
	}
	if wrongSC.Status != replaycoverage.StatusUnresolved {
		t.Errorf("fact_kind:gcp_cloud_resource (bound to odu:aws-pack) status = %q, want unresolved", wrongSC.Status)
	}
}
