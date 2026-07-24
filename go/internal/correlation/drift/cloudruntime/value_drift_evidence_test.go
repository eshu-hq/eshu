// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import "testing"

// TestBuildCandidatesEmitsDeclaredAndObservedValueDriftEvidence proves the
// image_version_drift candidate carries bounded declared_/observed_
// evidence atoms for the drifted attribute, sourced from the same
// ClassifyValueDrift result Classify used to pick the finding kind.
func TestBuildCandidatesEmitsDeclaredAndObservedValueDriftEvidence(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	rows := []AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_instance",
		Cloud: &ResourceRow{
			ARN:          arn,
			ResourceType: "aws_ec2_instance",
			Attributes:   map[string]string{"ami": "ami-000000000000000a"},
		},
		State: &ResourceRow{
			ARN:          arn,
			ResourceType: "aws_instance",
			Address:      "module.ecs.aws_instance.supply-chain-demo",
			Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
		},
		Config: &ResourceRow{ARN: arn, ResourceType: "aws_instance"},
	}}

	candidates := BuildCandidates(rows, "aws_account:123456789012:us-east-1")
	if len(candidates) != 1 {
		t.Fatalf("len(BuildCandidates()) = %d, want 1", len(candidates))
	}
	if got := findingKindValue(candidates[0]); got != string(FindingKindImageVersionDrift) {
		t.Fatalf("finding kind = %q, want %q", got, FindingKindImageVersionDrift)
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
		t.Fatalf("declared_ami evidence = %q, want %q", declared, "ami-0123456789abcdef0")
	}
	if observed != "ami-000000000000000a" {
		t.Fatalf("observed_ami evidence = %q, want %q", observed, "ami-000000000000000a")
	}
	if !hasEvidenceType(candidates[0], EvidenceTypeDeclaredValue) {
		t.Fatalf("candidate evidence missing %q", EvidenceTypeDeclaredValue)
	}
	if !hasEvidenceType(candidates[0], EvidenceTypeObservedValue) {
		t.Fatalf("candidate evidence missing %q", EvidenceTypeObservedValue)
	}
}

// TestBuildCandidatesOmitsValueDriftEvidenceWhenNoAttributesDiffer proves a
// converged (no-drift) resource never emits declared_/observed_ atoms.
func TestBuildCandidatesOmitsValueDriftEvidenceWhenNoAttributesDiffer(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-1"
	rows := []AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_instance",
		Cloud:        &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance", Attributes: map[string]string{"ami": "ami-a"}},
		State:        &ResourceRow{ARN: arn, ResourceType: "aws_instance", Attributes: map[string]string{"ami": "ami-a"}},
		Config:       &ResourceRow{ARN: arn, ResourceType: "aws_instance"},
	}}

	// No drift means Classify returns "" and BuildCandidates admits no
	// candidate at all for this row.
	candidates := BuildCandidates(rows, "aws_account:123456789012:us-east-1")
	if len(candidates) != 0 {
		t.Fatalf("len(BuildCandidates()) = %d, want 0 for a converged resource", len(candidates))
	}
}

// TestBuildCandidatesOrphanedResourceNeverEmitsValueDriftEvidence is a
// regression guard for existence-kind precedence at the evidence layer:
// an orphaned cloud resource has no state row, so ClassifyValueDrift must
// never be reachable and no declared_/observed_ atom must appear.
func TestBuildCandidatesOrphanedResourceNeverEmitsValueDriftEvidence(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-1"
	rows := []AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_instance",
		Cloud:        &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance", Attributes: map[string]string{"ami": "ami-a"}},
	}}

	candidates := BuildCandidates(rows, "aws_account:123456789012:us-east-1")
	if len(candidates) != 1 {
		t.Fatalf("len(BuildCandidates()) = %d, want 1", len(candidates))
	}
	if hasEvidenceType(candidates[0], EvidenceTypeDeclaredValue) {
		t.Fatalf("orphaned candidate must never carry %q evidence", EvidenceTypeDeclaredValue)
	}
	if hasEvidenceType(candidates[0], EvidenceTypeObservedValue) {
		t.Fatalf("orphaned candidate must never carry %q evidence", EvidenceTypeObservedValue)
	}
}
