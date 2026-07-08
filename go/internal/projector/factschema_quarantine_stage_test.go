// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage is the
// regression for the misattribution bug a typed-decode migration would
// otherwise introduce: buildCanonicalMaterialization merges quarantined facts
// from EVERY typed canonical extractor into one slice (terraform_state,
// oci_registry, and package_registry today), so a caller recording the visible
// input_invalid dead-letter must attribute each fact to the STAGE THAT ACTUALLY
// QUARANTINED IT, not a single hardcoded label. Before the terraform_state fix,
// runtime.go recorded the entire merged slice under ociRegistryCanonicalStage
// unconditionally — a terraform_state quarantine would have been mislabeled as
// an oci_registry_canonical failure in both the
// eshu_dp_projector_input_invalid_facts_total metric and the structured error
// log, misleading an operator investigating at 3am. This test now also proves
// quarantinedFactStagePrefixes' table shape scales to additional families
// (package_registry and codegraph) without prior families' routing regressing.
func TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage(t *testing.T) {
	t.Parallel()

	merged := []quarantinedFact{
		{factID: "tf-1", factKind: facts.TerraformStateResourceFactKind, field: "address"},
		{factID: "oci-1", factKind: facts.OCIImageManifestFactKind, field: "digest"},
		{factID: "tf-2", factKind: facts.TerraformStateTagObservationFactKind, field: "resource_address"},
		{factID: "pkg-1", factKind: facts.PackageRegistryPackageFactKind, field: "package_id"},
		{factID: "code-1", factKind: FactKindFileObserved, field: "relative_path"},
	}

	grouped := groupQuarantinedFactsByStage(merged)

	if len(grouped) != 4 {
		t.Fatalf("len(grouped) = %d, want 4 (terraform_state_canonical + oci_registry_canonical + package_registry_canonical + codegraph_canonical); got %+v", len(grouped), grouped)
	}

	tfGroup := grouped[terraformStateCanonicalStage]
	if len(tfGroup) != 2 {
		t.Fatalf("len(grouped[%q]) = %d, want 2", terraformStateCanonicalStage, len(tfGroup))
	}
	for _, q := range tfGroup {
		if q.factID != "tf-1" && q.factID != "tf-2" {
			t.Fatalf("terraform_state_canonical group carries unexpected fact %q; a sibling family's fact must not be misattributed to this stage", q.factID)
		}
	}

	ociGroup := grouped[ociRegistryCanonicalStage]
	if len(ociGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", ociRegistryCanonicalStage, len(ociGroup))
	}
	if ociGroup[0].factID != "oci-1" {
		t.Fatalf("oci_registry_canonical group carries fact %q, want oci-1; a sibling family's fact must not be misattributed to this stage", ociGroup[0].factID)
	}

	pkgGroup := grouped[packageRegistryCanonicalStage]
	if len(pkgGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", packageRegistryCanonicalStage, len(pkgGroup))
	}
	if pkgGroup[0].factID != "pkg-1" {
		t.Fatalf("package_registry_canonical group carries fact %q, want pkg-1; a sibling family's fact must not be misattributed to this stage", pkgGroup[0].factID)
	}

	codegraphGroup := grouped[codegraphCanonicalStage]
	if len(codegraphGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", codegraphCanonicalStage, len(codegraphGroup))
	}
	if codegraphGroup[0].factID != "code-1" {
		t.Fatalf("codegraph_canonical group carries fact %q, want code-1; a sibling family's fact must not be misattributed to this stage", codegraphGroup[0].factID)
	}
}

// TestGroupQuarantinedFactsByStageEmptyIsNil proves the empty-input no-op:
// recordProjectorQuarantinedFacts is safe to call zero times when nothing was
// quarantined (the common case), matching the pre-existing nil-safe contract.
func TestGroupQuarantinedFactsByStageEmptyIsNil(t *testing.T) {
	t.Parallel()

	if got := groupQuarantinedFactsByStage(nil); got != nil {
		t.Fatalf("groupQuarantinedFactsByStage(nil) = %+v, want nil", got)
	}
	if got := groupQuarantinedFactsByStage([]quarantinedFact{}); got != nil {
		t.Fatalf("groupQuarantinedFactsByStage(empty) = %+v, want nil", got)
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
		{facts.PackageRegistryPackageFactKind, packageRegistryCanonicalStage},
		{facts.PackageRegistryPackageDependencyFactKind, packageRegistryCanonicalStage},
		{facts.TerraformStateResourceFactKind, terraformStateCanonicalStage},
		{facts.OCIImageManifestFactKind, ociRegistryCanonicalStage},
		{facts.OCIRegistryRepositoryFactKind, ociRegistryCanonicalStage},
		{FactKindFileObserved, codegraphCanonicalStage},
		{"fileFact", codegraphCanonicalStage},
		{FactKindRepositoryObserved, codegraphCanonicalStage},
		{"repositoryFact", codegraphCanonicalStage},
		// A fact kind no prefix matches must fall back to the distinct unknown
		// stage, never to any family's own label.
		{"some_unwired_future_family.thing", unknownCanonicalStage},
		{"", unknownCanonicalStage},
	}

	// Repeat to catch any non-deterministic routing (the old randomized-map bug
	// this ordered slice fixes): every iteration must return the same stage.
	for i := 0; i < 64; i++ {
		for _, tc := range cases {
			if got := quarantinedFactStage(tc.factKind); got != tc.wantStage {
				t.Fatalf("quarantinedFactStage(%q) = %q, want %q (iteration %d)", tc.factKind, got, tc.wantStage, i)
			}
		}
	}
}
