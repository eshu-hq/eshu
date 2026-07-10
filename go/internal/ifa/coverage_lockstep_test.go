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
	// non-blocking gate cannot be trusted to keep a "covered" claim green. Post-P4
	// (#4397) ifa-contract-layer is blocking, real, and well-formed (local command
	// + CI workflow), so every referenced-proof_gate rule now holds and validation
	// returns no errors. Assert exactly that: this is the stronger check the P1
	// code could not make while the gate was deliberately advisory, and it fails
	// on a typo'd gate id, a lost local command, or a regression that flips
	// ifa-contract-layer back to advisory.
	if errs := replaycoverage.ValidateRequiredProofGates(ifaManifest, replaycoverage.AuthzProofLedger{}, proofGates); len(errs) != 0 {
		t.Errorf("ValidateRequiredProofGates = %v, want no errors now that ifa-contract-layer is blocking (#4397)", errs)
	}

	// Assert ifa-contract-layer's well-formedness directly against the registry
	// too, with clearer messages than the aggregate check above: after the P4 flip
	// (#4397) the gate must be blocking and carry both a local command and a CI
	// workflow. validateProofGate checks blocking before the command / workflow
	// checks and the shared precedence gap (#4959) still exists in
	// replaycoverage/proof_gates.go, so keeping these explicit invariants guards
	// against a future ordering-dependent regression in the aggregate validator.
	var ifaGate *cigates.Gate
	for i := range proofGates.Gates {
		if proofGates.Gates[i].ID == "ifa-contract-layer" {
			ifaGate = &proofGates.Gates[i]
		}
	}
	if ifaGate == nil {
		t.Fatal("ifa-contract-layer gate not found in ci-gates registry")
	}
	if !ifaGate.Blocking {
		t.Error("ifa-contract-layer must be CI-blocking after the P4 flip (#4397)")
	}
	if ifaGate.Local == nil || strings.TrimSpace(ifaGate.Local.Command) == "" {
		t.Error("ifa-contract-layer gate has no local command")
	}
	if strings.TrimSpace(ifaGate.CI.Workflow) == "" {
		t.Error("ifa-contract-layer gate has no CI workflow")
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

	// The five odu:demo-org-roundtrip rows use proof_gate: ifa-contract-layer
	// like every other row, so they add no proof-gate validation error (the gate
	// is blocking and well-formed, asserted above). ifa-contract-layer's
	// `go test ./internal/ifa` is what actually re-runs RoundTripTypedPayloads
	// on an ifa/synth-gcp change; the golden-corpus gate does not trigger on
	// those paths and replays the committed cassette, not the generator.

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
