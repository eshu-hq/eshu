// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import "testing"

// classifyExistenceOnly reproduces the pre-#5453 Classify body (existence
// dispatch only, no value-drift comparison) so BenchmarkClassifyExistenceOnly
// can serve as the "before" baseline for BenchmarkClassifyWithValueDrift
// without requiring git state manipulation to reconstruct the old function.
func classifyExistenceOnly(cloud, state, config *ResourceRow) FindingKind {
	if cloud == nil {
		return ""
	}
	if state == nil {
		return FindingKindOrphanedCloudResource
	}
	if config == nil {
		return FindingKindUnmanagedCloudResource
	}
	return ""
}

func benchmarkResourceRows() (*ResourceRow, *ResourceRow, *ResourceRow) {
	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	cloud := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Attributes:   map[string]string{"ami": "ami-000000000000000a"},
	}
	state := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
	}
	config := &ResourceRow{ARN: arn, ResourceType: "aws_instance"}
	return cloud, state, config
}

// BenchmarkClassifyExistenceOnly is the "before" baseline: the pre-#5453
// existence-only dispatch over the same fixture shape.
func BenchmarkClassifyExistenceOnly(b *testing.B) {
	cloud, state, config := benchmarkResourceRows()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyExistenceOnly(cloud, state, config)
	}
}

// BenchmarkClassifyWithValueDrift is the "after" measurement: the current
// Classify, which adds the ClassifyValueDrift attribute comparison once
// existence converges (#5453).
func BenchmarkClassifyWithValueDrift(b *testing.B) {
	cloud, state, config := benchmarkResourceRows()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Classify(cloud, state, config)
	}
}

// BenchmarkBuildCandidatesWithValueDrift measures the full candidate-build
// hot path (evidence assembly included) for one image_version_drift row,
// the realistic unit of work the reducer evaluates per ARN.
func BenchmarkBuildCandidatesWithValueDrift(b *testing.B) {
	cloud, state, config := benchmarkResourceRows()
	rows := []AddressedRow{{
		ARN:          cloud.ARN,
		ResourceType: state.ResourceType,
		Cloud:        cloud,
		State:        state,
		Config:       config,
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildCandidates(rows, "aws_account:123456789012:us-east-1")
	}
}
