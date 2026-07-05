// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage is the
// regression for the misattribution bug the terraform_state typed-decode
// migration would otherwise introduce: buildCanonicalMaterialization merges
// quarantined facts from EVERY typed canonical extractor into one slice
// (terraform_state and oci_registry today), so a caller recording the visible
// input_invalid dead-letter must attribute each fact to the STAGE THAT ACTUALLY
// QUARANTINED IT, not a single hardcoded label. Before this fix, runtime.go
// recorded the entire merged slice under ociRegistryCanonicalStage
// unconditionally — a terraform_state quarantine would have been mislabeled as
// an oci_registry_canonical failure in both the
// eshu_dp_projector_input_invalid_facts_total metric and the structured error
// log, misleading an operator investigating at 3am.
func TestGroupQuarantinedFactsByStageRoutesEachFamilyToItsOwnStage(t *testing.T) {
	t.Parallel()

	merged := []quarantinedFact{
		{factID: "tf-1", factKind: facts.TerraformStateResourceFactKind, field: "address"},
		{factID: "oci-1", factKind: facts.OCIImageManifestFactKind, field: "digest"},
		{factID: "tf-2", factKind: facts.TerraformStateTagObservationFactKind, field: "resource_address"},
	}

	grouped := groupQuarantinedFactsByStage(merged)

	if len(grouped) != 2 {
		t.Fatalf("len(grouped) = %d, want 2 (terraform_state_canonical + oci_registry_canonical); got %+v", len(grouped), grouped)
	}

	tfGroup := grouped[terraformStateCanonicalStage]
	if len(tfGroup) != 2 {
		t.Fatalf("len(grouped[%q]) = %d, want 2", terraformStateCanonicalStage, len(tfGroup))
	}
	for _, q := range tfGroup {
		if q.factID != "tf-1" && q.factID != "tf-2" {
			t.Fatalf("terraform_state_canonical group carries unexpected fact %q; the oci fact must not be misattributed to this stage", q.factID)
		}
	}

	ociGroup := grouped[ociRegistryCanonicalStage]
	if len(ociGroup) != 1 {
		t.Fatalf("len(grouped[%q]) = %d, want 1", ociRegistryCanonicalStage, len(ociGroup))
	}
	if ociGroup[0].factID != "oci-1" {
		t.Fatalf("oci_registry_canonical group carries fact %q, want oci-1; a terraform_state fact must not be misattributed to this stage", ociGroup[0].factID)
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
