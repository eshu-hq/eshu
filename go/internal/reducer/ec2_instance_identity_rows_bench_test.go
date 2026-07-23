// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractEC2InstanceIdentityNodeRows measures the in-memory
// projection of aws_ec2_instance aws_resource fact envelopes into
// deterministic identity-augment rows for a realistic per-scope-generation
// EC2 fleet size, mirroring BenchmarkExtractCloudResourceNodeRows
// (aws_resource_materialization_bench_test.go). This is the #5448
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

// BenchmarkExtractCloudResourceNodeRowsExcludesEC2InstanceFleet measures the
// SAME fleet size through the generic ExtractCloudResourceNodeRows path
// (aws_resource_materialization.go) to prove the #5448 exclusion adds no
// meaningful per-instance cost to the generic domain: every row decodes and
// is then skipped in O(1), producing zero output rows, so the generic
// domain's cost for an all-EC2-instance generation stays bounded rather than
// growing with a second full node-row build.
func BenchmarkExtractCloudResourceNodeRowsExcludesEC2InstanceFleet(b *testing.B) {
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
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, err := ExtractCloudResourceNodeRows(envelopes)
		if err != nil {
			b.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
		}
		if len(rows) != 0 {
			b.Fatalf("len(rows) = %d, want 0 (every row is an excluded aws_ec2_instance)", len(rows))
		}
	}
}
