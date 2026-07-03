// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestFactSchemaKindsMatchWireFactKinds locks each contracts-module fact-kind
// constant (factschema.FactKind*) to the wire fact-kind constant the collector
// emits and the reducer loads (facts.*FactKind). The contracts module is a
// standalone module that cannot import go/internal/facts, so it duplicates the
// wire strings as its own constants; this reducer-side test — which CAN import
// both packages — is the drift lock that keeps the two byte-equal.
//
// Without this lock a typo or a namespaced value (for example the Wave-1
// scaffold's "aws.resource" against the real "aws_resource") would make a Decode
// dispatch silently never match a loaded envelope: no error, no dead letter,
// just a fact kind that is never decoded. Every new decoded kind MUST add a row
// here so the mismatch is a test failure at authoring time.
func TestFactSchemaKindsMatchWireFactKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		contract string
		wireKind string
	}{
		{"aws_resource", factschema.FactKindAWSResource, facts.AWSResourceFactKind},
		{"aws_relationship", factschema.FactKindAWSRelationship, facts.AWSRelationshipFactKind},
		{"aws_security_group_rule", factschema.FactKindAWSSecurityGroupRule, facts.AWSSecurityGroupRuleFactKind},
		{"ec2_instance_posture", factschema.FactKindEC2InstancePosture, facts.EC2InstancePostureFactKind},
		{"s3_bucket_posture", factschema.FactKindS3BucketPosture, facts.S3BucketPostureFactKind},
		{"aws_iam_permission", factschema.FactKindAWSIAMPermission, facts.AWSIAMPermissionFactKind},
		{"aws_resource_policy_permission", factschema.FactKindAWSResourcePolicyPermission, facts.AWSResourcePolicyPermissionFactKind},
		{"aws_iam_principal", factschema.FactKindAWSIAMPrincipal, facts.AWSIAMPrincipalFactKind},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.contract != tc.wireKind {
				t.Fatalf("factschema constant %q != wire fact kind facts constant %q; the contracts-module fact-kind string has drifted from the reducer's wire kind and Decode dispatch will silently never match", tc.contract, tc.wireKind)
			}
		})
	}
}
