// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// TestBuildCandidatesSurfacesAWSValueDriftThroughMultiCloudPath proves the
// provider-neutral path reuses cloudruntime.Classify/ClassifyValueDrift
// verbatim: an AWS AMI mismatch admitted through the AWS-specific pack must
// also be classified image_version_drift, with the same declared_/observed_
// evidence, when routed through the multi-cloud Row/BuildCandidates path
// (#5453).
func TestBuildCandidatesSurfacesAWSValueDriftThroughMultiCloudPath(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	rows := []Row{{
		Provider:    cloudinventory.ProviderAWS,
		RawIdentity: arn,
		ScopeID:     "aws:123456789012:us-east-1:ec2",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_ec2_instance",
			Attributes:   map[string]string{"ami": "ami-000000000000000a"},
		},
		State: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_instance",
			Address:      "module.ecs.aws_instance.supply-chain-demo",
			Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
		},
		Config: &cloudruntime.ResourceRow{ARN: arn, ResourceType: "aws_instance"},
	}}

	candidates := BuildCandidates(rows, "multi")
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}
	if got := FindingKindFromCandidate(candidates[0]); got != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("finding kind = %q, want %q", got, cloudruntime.FindingKindImageVersionDrift)
	}

	var declared, observed string
	for _, atom := range candidates[0].Evidence {
		if atom.Key == "declared_ami" {
			declared = atom.Value
		}
		if atom.Key == "observed_ami" {
			observed = atom.Value
		}
	}
	if declared != "ami-0123456789abcdef0" {
		t.Fatalf("declared_ami = %q, want %q", declared, "ami-0123456789abcdef0")
	}
	if observed != "ami-000000000000000a" {
		t.Fatalf("observed_ami = %q, want %q", observed, "ami-000000000000000a")
	}
	if !hasEvidence(candidates[0], cloudruntime.EvidenceTypeDeclaredValue) {
		t.Fatalf("candidate evidence missing %q", cloudruntime.EvidenceTypeDeclaredValue)
	}
	if !hasEvidence(candidates[0], cloudruntime.EvidenceTypeObservedValue) {
		t.Fatalf("candidate evidence missing %q", cloudruntime.EvidenceTypeObservedValue)
	}
}
