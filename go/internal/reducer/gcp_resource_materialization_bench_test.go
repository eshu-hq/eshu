// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractGCPCloudResourceNodeRows measures the in-memory projection of
// gcp_cloud_resource fact envelopes into deterministic CloudResource node rows
// for a realistic per-scope-generation resource count. It mirrors the AWS node
// extraction benchmark: the cost must stay O(R) with no per-resource graph round
// trip, so the GCP node substrate (#2358) for the relationship edge projection
// (#2348) carries the same bounded build cost as the proven AWS path.
func BenchmarkExtractGCPCloudResourceNodeRows(b *testing.B) {
	const resourceCount = 5000
	envelopes := make([]facts.Envelope, 0, resourceCount)
	for i := 0; i < resourceCount; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.GCPCloudResourceFactKind,
			Payload: map[string]any{
				"full_resource_name": fmt.Sprintf("//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/vm-%d", i),
				"asset_type":         "compute.googleapis.com/Instance",
				"project_id":         "demo-proj",
				"location":           "us-central1-a",
				"asset_type_family":  "compute",
				"display_name":       fmt.Sprintf("vm-%d", i),
				"state":              "RUNNING",
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, err := ExtractGCPCloudResourceNodeRows(envelopes)
		if err != nil {
			b.Fatalf("ExtractGCPCloudResourceNodeRows() error = %v, want nil", err)
		}
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}
