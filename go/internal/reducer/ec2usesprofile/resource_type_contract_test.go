// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2usesprofile

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// TestResourceTypeConstantsMatchFactSchema keeps the contract-mirror coverage
// the root TestReducerAWSResourceTypeConstantsMatchFactSchema provided before
// the #6061 move: the family's resource-type tokens must stay identical to the
// canonical factschema constants. Both are `= awsv1.…` aliases, so drift
// requires editing the const line itself — this test makes that edit fail
// loudly instead of silently.
func TestResourceTypeConstantsMatchFactSchema(t *testing.T) {
	t.Parallel()

	if ec2UsesProfileResourceTypeInstanceProfile != awsv1.ResourceTypeIAMInstanceProfile {
		t.Fatalf(
			"instance-profile resource type = %q, want %q",
			ec2UsesProfileResourceTypeInstanceProfile,
			awsv1.ResourceTypeIAMInstanceProfile,
		)
	}
	if ec2UsesProfileResourceTypeInstance != awsv1.ResourceTypeEC2Instance {
		t.Fatalf(
			"instance resource type = %q, want %q",
			ec2UsesProfileResourceTypeInstance,
			awsv1.ResourceTypeEC2Instance,
		)
	}
}
