// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkCloudTagEvidenceRecordFromRow measures cloudTagEvidenceRecordFromRow
// (#4686) on one representative azure_tag_observation row and one
// gcp_tag_observation row per iteration — the same function signature before
// and after the typed-decode conversion, so this benchmark runs unmodified
// against both the pre-change (raw map[string]any + coerceJSONString) and
// post-change (sdk/go/factschema typed decode) implementations. See
// go/internal/storage/postgres/AGENTS.md for the recorded before/after
// measurement (No-Regression Evidence, #4686).
func BenchmarkCloudTagEvidenceRecordFromRow(b *testing.B) {
	azureID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-bench"
	azurePayload := []byte(`{
		"arm_resource_id":"` + azureID + `",
		"normalized_resource_id":"` + azureID + `",
		"resource_type":"Microsoft.Compute/virtualMachines",
		"tag_value_fingerprints":{"env":"az-env-marker","owner":"az-owner-marker","team":"az-team-marker"}
	}`)
	gcpID := "//compute.googleapis.com/projects/proj-1/zones/us-central1-a/instances/vm-bench"
	gcpPayload := []byte(`{
		"full_resource_name":"` + gcpID + `",
		"asset_type":"compute.googleapis.com/Instance",
		"tag_value_fingerprints":{"env":"gcp-env-marker","owner":"gcp-owner-marker","team":"gcp-team-marker"}
	}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cloudTagEvidenceRecordFromRow(facts.AzureTagObservationFactKind, azureID, azurePayload); !ok {
			b.Fatal("azure row unexpectedly dropped")
		}
		if _, ok := cloudTagEvidenceRecordFromRow(facts.GCPTagObservationFactKind, gcpID, gcpPayload); !ok {
			b.Fatal("gcp row unexpectedly dropped")
		}
	}
}
