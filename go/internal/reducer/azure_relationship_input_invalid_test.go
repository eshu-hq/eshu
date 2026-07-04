// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAzureRelationshipMaterializationQuarantinesMissingResourceType is the
// flagship regression test for Wave 4a of Contract System v1 (issue #4566): the
// AZURE cloud family's typed-decode migration. It proves the accuracy guarantee
// the migration exists to protect AND the per-fact isolation contract the AWS
// migration (#4568) established: an azure_cloud_resource fact missing its
// required resource_type key is QUARANTINED as a visible input_invalid
// dead-letter — never silently indexed under a uid computed with an
// empty-string resource_type segment — while every VALID fact in the same batch
// still projects and the handler continues (per-fact isolation, not a
// whole-intent failure).
//
// Before the migration this behavior was impossible: azureCloudResourceNodeRow
// read resource_type with payloadString, which returns "" for the absent key,
// and cloudResourceUID(subscriptionID, location, "", resourceID) yielded a
// wrong-but-plausible identity that silently entered the join index — the
// silent wrong graph truth the Life Motto ranks as the worst failure.
//
// After the migration ExtractAzureCloudResourceNodeRows decodes each
// azure_cloud_resource fact through factschema.DecodeAzureCloudResource; the
// malformed fact yields a classified *factschema.DecodeError that
// partitionDecodeFailures routes to a per-fact quarantine. The row extractor
// skips the malformed fact and continues, so the batch's valid resource still
// materializes and no row references an empty-resource_type uid.
func TestAzureRelationshipMaterializationQuarantinesMissingResourceType(t *testing.T) {
	t.Parallel()

	// A resource fact whose required resource_type key is ABSENT (not merely
	// empty): the exact malformed input the AC names. Everything else is
	// present so the ONLY reason to quarantine the fact is the missing required
	// field.
	malformed := azureResourceEnvelope(map[string]any{
		// "resource_type" intentionally absent.
		"arm_resource_id":        azureVMID,
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
		"subscription_id":        "sub-1",
		"location":               "eastus",
	})
	// A fully valid, independent resource that must still project despite the
	// malformed fact sharing the batch. This is the isolation half of the
	// contract: valid facts are unaffected by a poisoned sibling.
	valid := azureNICResource()

	rows, quarantined, err := ExtractAzureCloudResourceNodeRows([]facts.Envelope{malformed, valid})
	if err != nil {
		t.Fatalf("ExtractAzureCloudResourceNodeRows returned error %v; a single malformed azure_cloud_resource fact must be quarantined per-fact, not fail the whole batch", err)
	}

	// Per-fact isolation: the malformed fact does NOT abort the whole batch —
	// exactly one quarantine is recorded and the valid resource still projects.
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-resource_type fact must be recorded as one input_invalid quarantine", len(quarantined))
	}
	if quarantined[0].field != "resource_type" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "resource_type")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; the valid resource must still materialize despite the quarantined fact", len(rows))
	}

	// No row may reference a uid computed with an empty-string resource_type
	// segment — the accuracy guarantee.
	emptyTypeUID := cloudResourceUID("sub-1", "eastus", "", "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm")
	for _, row := range rows {
		if row["uid"] == emptyTypeUID {
			t.Fatalf("written row references the empty-resource_type uid %q; a quarantined fact must never produce graph identity", emptyTypeUID)
		}
	}
}
