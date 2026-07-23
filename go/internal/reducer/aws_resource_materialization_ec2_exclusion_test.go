// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCloudResourceNodeRowsExcludesEC2Instance is the #5448 CRUX-1
// exclusion proof at the extraction boundary: an aws_ec2_instance aws_resource
// fact never produces a base CloudResource node row here, even though it
// carries a complete, valid identity. DomainEC2InstanceNodeMaterialization
// already owns creation of that node's base properties from the
// ec2_instance_posture fact; letting this generic path also write them would
// race two reducer domains over the same property set. See the doc comment on
// cloudResourceNodeRow (aws_resource_materialization.go) and
// TestEC2InstanceIdentityMaterializationDoesNotDisturbPostureNode
// (ec2_instance_identity_materialization_test.go).
func TestExtractCloudResourceNodeRowsExcludesEC2Instance(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_instance",
			"resource_id":   "i-0000000000000000a",
			"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000a",
			"name":          "i-0000000000000000a",
			"state":         "running",
		}),
		awsResourceEnvelope(map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-123",
		}),
	}

	rows, quarantined, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0 (exclusion is not a decode failure)", len(quarantined))
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (only the non-instance resource)", len(rows))
	}
	if got := anyToString(rows[0]["resource_type"]); got != "aws_ec2_vpc" {
		t.Fatalf("resource_type = %q, want aws_ec2_vpc (the EC2 instance row must be excluded)", got)
	}
}
