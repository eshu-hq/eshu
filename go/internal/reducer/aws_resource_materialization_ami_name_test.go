// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCloudResourceNodeRowsAMIRogueAttributesNameStaysBareID settles
// #9525 decision 7d: the claimed graph-node Name bypass does not exist. The
// admission writer copies the attributes map verbatim into the canonical
// payload (cloud_inventory_admission_writer.go), but the CloudResource node
// Name comes from the typed top-level Name field only
// (cloudResourceNodeRow: derefString(resource.Name)), never from
// attributes["name"]. Even a rogue source payload carrying a fancy name and
// a description inside attributes still materializes with the bare AMI id,
// which is what the producer sets as top-level Name. Negative case per the
// correlation proof matrix: rogue evidence stays non-materialized.
func TestExtractCloudResourceNodeRowsAMIRogueAttributesNameStaysBareID(t *testing.T) {
	t.Parallel()

	const amiID = "ami-0000000000000000a"
	envelopes := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_ami",
			"resource_id":   amiID,
			"arn":           "",
			"name":          amiID,
			"attributes": map[string]any{
				"name":        "rogue-fancy-name (prod)",
				"description": "must-not-surface",
			},
		}),
	}
	rows, _, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 AMI node", len(rows))
	}
	if got := rows[0]["name"]; got != amiID {
		t.Fatalf("node name = %#v, want bare AMI id %q", got, amiID)
	}
	if _, present := rows[0]["description"]; present {
		t.Fatalf("node carries description: %#v", rows[0])
	}
}
