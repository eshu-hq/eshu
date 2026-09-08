// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2instance

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractEC2InstanceIdentityNodeRows measures the in-memory
// projection of aws_ec2_instance aws_resource fact envelopes into
// deterministic identity-augment rows for a realistic per-scope-generation
// EC2 fleet size, mirroring BenchmarkExtractCloudResourceNodeRows
// (aws_resource_materialization_bench_test.go, reducer root). This is the #5448
// fleet-scale fan-out cost: it must stay O(N) in instance count with no
// per-instance graph round trip, matching the existing extractor's shape.
func BenchmarkExtractEC2InstanceIdentityNodeRows(b *testing.B) {
	const instanceCount = 5000
	envelopes := make([]facts.Envelope, 0, instanceCount)
	for i := 0; i < instanceCount; i++ {
		instanceID := fmt.Sprintf("i-%016d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("fact-identity-%d", i),
			FactKind: facts.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id":    "111122223333",
				"region":        "us-east-1",
				"resource_type": "aws_ec2_instance",
				"resource_id":   instanceID,
				"arn":           "arn:aws:ec2:us-east-1:111122223333:instance/" + instanceID,
				"name":          instanceID,
				"state":         "running",
				"attributes": map[string]any{
					"ami_id": fmt.Sprintf("ami-%016d", i%50),
				},
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
		if err != nil {
			b.Fatalf("ExtractEC2InstanceIdentityNodeRows() error = %v, want nil", err)
		}
		if len(rows) != instanceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), instanceCount)
		}
	}
}
