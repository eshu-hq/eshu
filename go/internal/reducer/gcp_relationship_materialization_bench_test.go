// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractGCPRelationshipEdgeRows measures the bounded in-memory join of
// gcp_cloud_relationship facts against the gcp_cloud_resource join index for a
// realistic per-scope-generation cardinality. Resolution is a single exact map
// lookup per endpoint (no per-edge graph round trip), so the cost must stay O(R+E)
// — mirroring the AWS relationship edge extraction shape.
func BenchmarkExtractGCPRelationshipEdgeRows(b *testing.B) {
	const resourceCount = 5000
	resources := make([]facts.Envelope, 0, resourceCount)
	rels := make([]facts.Envelope, 0, resourceCount)
	for i := 0; i < resourceCount; i++ {
		instance := fmt.Sprintf("//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/vm-%d", i)
		disk := fmt.Sprintf("//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/disks/disk-%d", i)
		resources = append(
			resources,
			facts.Envelope{FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
				"full_resource_name": instance,
				"asset_type":         "compute.googleapis.com/Instance",
				"project_id":         "demo-proj",
				"location":           "us-central1-a",
			}},
			facts.Envelope{FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
				"full_resource_name": disk,
				"asset_type":         "compute.googleapis.com/Disk",
				"project_id":         "demo-proj",
				"location":           "us-central1-a",
			}},
		)
		rels = append(rels, facts.Envelope{FactKind: facts.GCPCloudRelationshipFactKind, Payload: map[string]any{
			"source_full_resource_name": instance,
			"target_full_resource_name": disk,
			"relationship_type":         "INSTANCE_TO_DISK",
			"target_asset_type":         "compute.googleapis.com/Disk",
			"support_state":             "supported",
		}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := ExtractGCPRelationshipEdgeRows(resources, rels)
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}
