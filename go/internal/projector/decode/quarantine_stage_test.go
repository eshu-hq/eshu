// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage is the
// regression for the misattribution bug a typed-decode migration would
// otherwise introduce: BuildMaterialization merges quarantined facts
// from EVERY typed canonical extractor into one slice (terraform_state,
// oci_registry, and package_registry today), so a caller recording the visible
// input_invalid dead-letter must attribute each fact to the STAGE THAT ACTUALLY
// QUARANTINED IT, not a single hardcoded label. Before the terraform_state fix,
// runtime.go recorded the entire merged slice under OCIRegistryCanonicalStage
// unconditionally — a terraform_state quarantine would have been mislabeled as
// an oci_registry_canonical failure in both the
// eshu_dp_projector_input_invalid_facts_total metric and the structured error
// log, misleading an operator investigating at 3am. This test now also proves
// quarantinedFactStagePrefixes' table shape scales to additional families
// (package_registry and codegraph) without prior families' routing regressing.
func TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage(t *testing.T) {
	t.Parallel()

	merged := []QuarantinedFact{
		{FactID: "tf-1", FactKind: facts.TerraformStateResourceFactKind, Field: "address"},
		{FactID: "oci-1", FactKind: facts.OCIImageManifestFactKind, Field: "digest"},
		{FactID: "tf-2", FactKind: facts.TerraformStateTagObservationFactKind, Field: "resource_address"},
		{FactID: "pkg-1", FactKind: facts.PackageRegistryPackageFactKind, Field: "package_id"},
		{FactID: "code-1", FactKind: FactKindFileObserved, Field: "relative_path"},
	}

	grouped := GroupQuarantinedFactsByStage(merged)

	if len(grouped) != 4 {
		t.Fatalf("len(grouped) = %d, want 4 (terraform_state_canonical + oci_registry_canonical + package_registry_canonical + codegraph_canonical); got %+v", len(grouped), grouped)
	}

	tfGroup := grouped[TerraformStateCanonicalStage]
	if len(tfGroup) != 2 {
		t.Fatalf("len(grouped[%q]) = %d, want 2", TerraformStateCanonicalStage, len(tfGroup))
	}
	for _, q := range tfGroup {
		if q.FactID != "tf-1" && q.FactID != "tf-2" {
			t.Fatalf("terraform_state_canonical group carries unexpected fact %q; a sibling family's fact must not be misattributed to this stage", q.FactID)
		}
	}

	ociGroup := grouped[OCIRegistryCanonicalStage]
	if len(ociGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", OCIRegistryCanonicalStage, len(ociGroup))
	}
	if ociGroup[0].FactID != "oci-1" {
		t.Fatalf("oci_registry_canonical group carries fact %q, want oci-1; a sibling family's fact must not be misattributed to this stage", ociGroup[0].FactID)
	}

	pkgGroup := grouped[PackageRegistryCanonicalStage]
	if len(pkgGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", PackageRegistryCanonicalStage, len(pkgGroup))
	}
	if pkgGroup[0].FactID != "pkg-1" {
		t.Fatalf("package_registry_canonical group carries fact %q, want pkg-1; a sibling family's fact must not be misattributed to this stage", pkgGroup[0].FactID)
	}

	codegraphGroup := grouped[CodegraphCanonicalStage]
	if len(codegraphGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", CodegraphCanonicalStage, len(codegraphGroup))
	}
	if codegraphGroup[0].FactID != "code-1" {
		t.Fatalf("codegraph_canonical group carries fact %q, want code-1; a sibling family's fact must not be misattributed to this stage", codegraphGroup[0].FactID)
	}
}

// TestGroupQuarantinedFactsByStageEmptyIsNil proves the empty-input no-op:
// RecordQuarantinedFacts is safe to call zero times when nothing was
// quarantined (the common case), matching the pre-existing nil-safe contract.
func TestGroupQuarantinedFactsByStageEmptyIsNil(t *testing.T) {
	t.Parallel()

	if got := GroupQuarantinedFactsByStage(nil); got != nil {
		t.Fatalf("GroupQuarantinedFactsByStage(nil) = %+v, want nil", got)
	}
	if got := GroupQuarantinedFactsByStage([]QuarantinedFact{}); got != nil {
		t.Fatalf("GroupQuarantinedFactsByStage(empty) = %+v, want nil", got)
	}
}

// TestQuarantinedFactStageRoutesAndFallsBack locks the deterministic
// prefix-to-stage routing (an ordered slice, not a randomized Go map) and the
// explicit unknown-stage fallback: a fact kind matching no known prefix routes
// to unknownCanonicalStage — a distinct, operator-honest label — rather than
// silently borrowing another family's stage. It runs the routing many times so
// a non-deterministic map-iteration regression (the old shape) would flake.
func TestQuarantinedFactStageRoutesAndFallsBack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		factKind  string
		wantStage string
	}{
		{facts.PackageRegistryPackageFactKind, PackageRegistryCanonicalStage},
		{facts.PackageRegistryPackageDependencyFactKind, PackageRegistryCanonicalStage},
		{facts.TerraformStateResourceFactKind, TerraformStateCanonicalStage},
		{facts.OCIImageManifestFactKind, OCIRegistryCanonicalStage},
		{facts.OCIRegistryRepositoryFactKind, OCIRegistryCanonicalStage},
		{FactKindFileObserved, CodegraphCanonicalStage},
		{"fileFact", CodegraphCanonicalStage},
		{FactKindRepositoryObserved, CodegraphCanonicalStage},
		{"repositoryFact", CodegraphCanonicalStage},
		// A fact kind no prefix matches must fall back to the distinct unknown
		// stage, never to any family's own label.
		{"some_unwired_future_family.thing", unknownCanonicalStage},
		{"", unknownCanonicalStage},
	}

	// Repeat to catch any non-deterministic routing (the old randomized-map bug
	// this ordered slice fixes): every iteration must return the same stage.
	for i := 0; i < 64; i++ {
		for _, tc := range cases {
			if got := QuarantinedFactStage(tc.factKind); got != tc.wantStage {
				t.Fatalf("QuarantinedFactStage(%q) = %q, want %q (iteration %d)", tc.factKind, got, tc.wantStage, i)
			}
		}
	}
}
