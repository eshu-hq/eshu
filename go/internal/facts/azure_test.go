// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestAzureFactKindRegistry(t *testing.T) {
	kinds := AzureFactKinds()
	want := []string{
		AzureCloudResourceFactKind,
		AzureCloudRelationshipFactKind,
		AzureTagObservationFactKind,
		AzureIdentityObservationFactKind,
		AzureResourceChangeFactKind,
		AzureDNSRecordFactKind,
		AzureImageReferenceFactKind,
		AzureCollectionWarningFactKind,
	}
	if len(kinds) != len(want) {
		t.Fatalf("len(AzureFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("AzureFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := AzureSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("AzureSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != "1.0.0" {
			t.Fatalf("AzureSchemaVersion(%q) = %q, want 1.0.0", kinds[i], version)
		}
	}

	if _, ok := AzureSchemaVersion("azure_not_a_kind"); ok {
		t.Fatal("AzureSchemaVersion returned ok for an unknown kind")
	}

	kinds[0] = "mutated"
	if got := AzureFactKinds()[0]; got != AzureCloudResourceFactKind {
		t.Fatalf("AzureFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestAzureFactKindValues(t *testing.T) {
	cases := map[string]string{
		AzureCloudResourceFactKind:       "azure_cloud_resource",
		AzureCloudRelationshipFactKind:   "azure_cloud_relationship",
		AzureTagObservationFactKind:      "azure_tag_observation",
		AzureIdentityObservationFactKind: "azure_identity_observation",
		AzureResourceChangeFactKind:      "azure_resource_change",
		AzureDNSRecordFactKind:           "azure_dns_record",
		AzureImageReferenceFactKind:      "azure_image_reference",
		AzureCollectionWarningFactKind:   "azure_collection_warning",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("fact kind constant = %q, want %q", got, want)
		}
	}
}
