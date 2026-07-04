// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractAzureCloudResourceNodeRows measures the in-memory projection
// of azure_cloud_resource fact envelopes into deterministic CloudResource node
// rows for a realistic per-scope-generation resource count. This is the
// bounded typed-decode cost the AWS migration's No-Regression Evidence
// established a ~10% diagnostic band for; it must stay O(R) with no
// per-resource graph round trip.
func BenchmarkExtractAzureCloudResourceNodeRows(b *testing.B) {
	const resourceCount = 5000
	envelopes := make([]facts.Envelope, 0, resourceCount)
	for i := 0; i < resourceCount; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.AzureCloudResourceFactKind,
			Payload: map[string]any{
				"arm_resource_id":        fmt.Sprintf("/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-%d", i),
				"normalized_resource_id": fmt.Sprintf("/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm-%d", i),
				"subscription_id":        "sub-1",
				"resource_type":          "microsoft.compute/virtualmachines",
				"resource_name":          fmt.Sprintf("vm-%d", i),
				"location":               "eastus",
				"provider_namespace":     "microsoft.compute",
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, quarantined, err := ExtractAzureCloudResourceNodeRows(envelopes)
		if err != nil {
			b.Fatalf("ExtractAzureCloudResourceNodeRows() error = %v, want nil", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("len(quarantined) = %d, want 0", len(quarantined))
		}
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}

// BenchmarkExtractAzureRelationshipEdgeRows measures the in-memory join-index
// build plus edge projection for azure_cloud_relationship facts against their
// resolving azure_cloud_resource facts, mirroring
// BenchmarkExtractAWSRelationshipEdgeRows's shape for the Azure family.
func BenchmarkExtractAzureRelationshipEdgeRows(b *testing.B) {
	const resourceCount = 2500
	resourceEnvelopes := make([]facts.Envelope, 0, resourceCount)
	relationshipEnvelopes := make([]facts.Envelope, 0, resourceCount/2)
	for i := 0; i < resourceCount; i++ {
		armID := fmt.Sprintf("/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-%d", i)
		normalizedID := fmt.Sprintf("/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm-%d", i)
		resourceEnvelopes = append(resourceEnvelopes, facts.Envelope{
			FactKind: facts.AzureCloudResourceFactKind,
			Payload: map[string]any{
				"arm_resource_id":        armID,
				"normalized_resource_id": normalizedID,
				"subscription_id":        "sub-1",
				"resource_type":          "microsoft.compute/virtualmachines",
				"location":               "eastus",
			},
		})
		if i%2 == 1 {
			prevNormalizedID := fmt.Sprintf("/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm-%d", i-1)
			relationshipEnvelopes = append(relationshipEnvelopes, facts.Envelope{
				FactKind: facts.AzureCloudRelationshipFactKind,
				Payload: map[string]any{
					"source_arm_resource_id":        armID,
					"target_arm_resource_id":        fmt.Sprintf("/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-%d", i-1),
					"source_normalized_resource_id": normalizedID,
					"target_normalized_resource_id": prevNormalizedID,
					"relationship_type":             "managed_by",
					"target_resource_type":          "microsoft.compute/virtualmachines",
					"support_state":                 "supported",
				},
			})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, quarantined, err := ExtractAzureRelationshipEdgeRows(resourceEnvelopes, relationshipEnvelopes)
		if err != nil {
			b.Fatalf("ExtractAzureRelationshipEdgeRows() error = %v, want nil", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("len(quarantined) = %d, want 0", len(quarantined))
		}
		if len(rows) != len(relationshipEnvelopes) {
			b.Fatalf("len(rows) = %d, want %d", len(rows), len(relationshipEnvelopes))
		}
	}
}
