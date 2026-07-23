// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

const (
	testEC2IdentityAccount = "123456789012"
	testEC2IdentityRegion  = "us-east-1"
)

func ec2InstanceIdentityEnvelope(instanceID, arn, amiID string) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-identity-" + instanceID,
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    testEC2IdentityAccount,
			"region":        testEC2IdentityRegion,
			"resource_type": awsv1.ResourceTypeEC2Instance,
			"resource_id":   instanceID,
			"arn":           arn,
			"name":          instanceID,
			"state":         "running",
			"attributes": map[string]any{
				"ami_id": amiID,
			},
		},
	}
}

func ec2InstanceIdentityUID(instanceID string) string {
	return cloudResourceUID(testEC2IdentityAccount, testEC2IdentityRegion, awsv1.ResourceTypeEC2Instance, instanceID)
}

func TestExtractEC2InstanceIdentityNodeRowsProjectsAMIID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ec2InstanceIdentityEnvelope("i-0000000000000000a", "arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000a", "ami-0000000000000000a"),
	}

	rows, quarantined, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	wantUID := ec2InstanceIdentityUID("i-0000000000000000a")
	if rows[0]["uid"] != wantUID {
		t.Fatalf("uid = %v, want %v", rows[0]["uid"], wantUID)
	}
	if rows[0]["ami_id"] != "ami-0000000000000000a" {
		t.Fatalf("ami_id = %v, want ami-0000000000000000a", rows[0]["ami_id"])
	}
	// The provenance key MUST be "source_fact_id" — this is the exact key the
	// writer's Cypher reads as row.source_fact_id to persist
	// r.ec2_identity_source_fact_id. A rename here would silently null out the
	// provenance property at write time (the extractor->writer seam has no
	// other guard), so this assertion is the cross-seam contract.
	if rows[0]["source_fact_id"] != "fact-identity-i-0000000000000000a" {
		t.Fatalf("source_fact_id = %v, want fact-identity-i-0000000000000000a", rows[0]["source_fact_id"])
	}
	if _, exists := rows[0]["ec2_identity_source_fact_id"]; exists {
		t.Fatalf("row carried the graph-property name as a row key; the writer Cypher reads row.source_fact_id, not row.ec2_identity_source_fact_id")
	}
	// The row MUST NOT carry any base identity/posture field the
	// ec2_instance_posture node materialization already owns — disjointness is
	// what makes the dual-domain write safe (see aws_resource_materialization.go's
	// cloudResourceNodeRow exclusion doc).
	for _, forbidden := range []string{"arn", "resource_id", "resource_type", "name", "state", "account_id", "region", "service_kind", "correlation_anchors"} {
		if _, exists := rows[0][forbidden]; exists {
			t.Fatalf("row carried forbidden base field %q: %#v", forbidden, rows[0])
		}
	}
}

func TestExtractEC2InstanceIdentityNodeRowsIgnoresNonEC2InstanceResources(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-resource-vpc",
			FactKind: facts.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id":    testEC2IdentityAccount,
				"region":        testEC2IdentityRegion,
				"resource_type": "aws_ec2_vpc",
				"resource_id":   "vpc-123",
			},
		},
	}

	rows, _, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for a non-EC2-instance resource_type", len(rows))
	}
}

func TestExtractEC2InstanceIdentityNodeRowsSkipsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-identity-empty",
			FactKind: facts.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id":    testEC2IdentityAccount,
				"region":        testEC2IdentityRegion,
				"resource_type": awsv1.ResourceTypeEC2Instance,
				"resource_id":   "",
				"arn":           "",
			},
		},
	}

	rows, quarantined, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 || len(quarantined) != 0 {
		t.Fatalf("rows = %d quarantined = %d, want 0/0 for an incomplete identity (never fabricate)", len(rows), len(quarantined))
	}
}

func TestExtractEC2InstanceIdentityNodeRowsSkipsEmptyAMI(t *testing.T) {
	t.Parallel()

	// A well-formed instance (valid uid) that reports no ImageId must NOT stamp
	// ami_id="" onto the posture-owned CloudResource node: an empty-but-present
	// property would falsely satisfy a `WHERE r.ami_id IS NOT NULL` reader. The
	// scope-wide retract-first still removes any stale ami_id, so the correct
	// behavior is to skip the write row entirely.
	envelopes := []facts.Envelope{
		ec2InstanceIdentityEnvelope("i-0000000000000000c", "arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000c", "  "),
	}

	rows, quarantined, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 || len(quarantined) != 0 {
		t.Fatalf("rows = %d quarantined = %d, want 0/0 for an instance with no observed ami_id", len(rows), len(quarantined))
	}
}

func TestExtractEC2InstanceIdentityNodeRowsSkipsTombstones(t *testing.T) {
	t.Parallel()

	envelope := ec2InstanceIdentityEnvelope("i-0000000000000000b", "arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000b", "ami-0000000000000000b")
	envelope.IsTombstone = true

	rows, _, err := ExtractEC2InstanceIdentityNodeRows([]facts.Envelope{envelope})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for a tombstoned fact", len(rows))
	}
}
