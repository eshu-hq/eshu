// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractCloudResourceNodeRows measures the in-memory projection of
// aws_resource fact envelopes into deterministic CloudResource node rows for a
// realistic per-scope-generation resource count. This is the bounded join-index
// build cost; it must stay O(R) with no per-resource graph round trip.
func BenchmarkExtractCloudResourceNodeRows(b *testing.B) {
	const resourceCount = 5000
	envelopes := make([]facts.Envelope, 0, resourceCount)
	for i := 0; i < resourceCount; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id":          "111122223333",
				"region":              "us-east-1",
				"resource_type":       "aws_ec2_vpc",
				"resource_id":         fmt.Sprintf("vpc-%d", i),
				"name":                "main",
				"correlation_anchors": []any{fmt.Sprintf("vpc-%d", i)},
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := ExtractCloudResourceNodeRows(envelopes)
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}
