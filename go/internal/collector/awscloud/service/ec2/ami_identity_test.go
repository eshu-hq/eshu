// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestScannerDedupesAMIResourceFactAcrossSharedInstances proves the #5717 AMI
// resource fact is emitted once per distinct (account, region, image id) per
// scan, not once per instance: many instances commonly share one AMI, and
// emitting a duplicate fact per instance would bloat fact volume for no
// identity benefit (the reducer's uid-keyed materialization would collapse
// them anyway, but the collector should not manufacture the duplicates in the
// first place). Split out of scanner_test.go to stay under the repository's
// 500-line-per-file cap; shares fakeClient/testBoundary/factKindCounts with
// the rest of the package's tests.
func TestScannerDedupesAMIResourceFactAcrossSharedInstances(t *testing.T) {
	client := fakeClient{
		instances: []Instance{
			{
				ID:      "i-1111111111111111a",
				ARN:     "arn:aws:ec2:us-east-1:123456789012:instance/i-1111111111111111a",
				State:   "running",
				ImageID: "ami-0000000000000000a",
			},
			{
				ID:      "i-2222222222222222b",
				ARN:     "arn:aws:ec2:us-east-1:123456789012:instance/i-2222222222222222b",
				State:   "running",
				ImageID: "ami-0000000000000000a",
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	// 2 instance identity facts + 1 deduped AMI resource fact.
	if counts := factKindCounts(envelopes); counts[facts.AWSResourceFactKind] != 3 {
		t.Fatalf("aws_resource count = %d, want 3 (2 instance identities + 1 deduped AMI resource fact)", counts[facts.AWSResourceFactKind])
	}
	amiResourceCount := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == awscloud.ResourceTypeEC2AMI {
			amiResourceCount++
		}
	}
	if amiResourceCount != 1 {
		t.Fatalf("AMI resource fact count = %d, want 1 (deduped across 2 instances sharing the same AMI)", amiResourceCount)
	}
	// Both instances still resolve their own instance->AMI relationship.
	if counts := factKindCounts(envelopes); counts[facts.AWSRelationshipFactKind] != 2 {
		t.Fatalf("aws_relationship count = %d, want 2 (one per instance)", counts[facts.AWSRelationshipFactKind])
	}
}
