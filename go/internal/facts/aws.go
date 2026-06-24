// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// AWSResourceFactKind identifies one resource reported by an AWS API.
	AWSResourceFactKind = "aws_resource"
	// AWSRelationshipFactKind identifies one relationship reported by AWS APIs.
	AWSRelationshipFactKind = "aws_relationship"
	// AWSTagObservationFactKind identifies one raw AWS tag observation.
	AWSTagObservationFactKind = "aws_tag_observation"
	// AWSDNSRecordFactKind identifies one Route53 DNS record observation.
	AWSDNSRecordFactKind = "aws_dns_record"
	// AWSImageReferenceFactKind identifies one ECR image reference observation.
	AWSImageReferenceFactKind = "aws_image_reference"
	// AWSSecurityGroupRuleFactKind identifies one normalized EC2 security-group
	// ingress or egress rule. It is a derived posture fact distinct from the raw
	// aws_resource security-group-rule observation: each fact carries the single
	// normalized reachability tuple (group, direction, protocol, port range,
	// source) that the reducer projects into network-reachability edges.
	AWSSecurityGroupRuleFactKind = "aws_security_group_rule"
	// AWSIAMPermissionFactKind identifies one derived IAM permission statement.
	//
	// It is the normalized, metadata-only projection of a single IAM policy
	// statement attached to a principal: effect, action set, resource pattern,
	// and a condition summary. It NEVER carries the raw policy JSON body or any
	// condition values. PR1 emits this fact; the reducer graph projection that
	// consumes it ships separately under principal review (issue #1134).
	AWSIAMPermissionFactKind = "aws_iam_permission"
	// AWSResourcePolicyPermissionFactKind identifies one derived
	// resource-based-policy permission statement.
	//
	// It is the resource-side analog of aws_iam_permission: the normalized,
	// metadata-only projection of a single statement from a resource policy
	// attached to an AWS resource (an S3 bucket policy or a KMS key policy). It
	// captures the attached resource identity, the statement effect, the
	// normalized action/resource patterns, a condition-key summary, and the
	// derived grantee principal facts (principal account ids, principal types,
	// public/anonymous, cross-account). It NEVER carries the raw policy JSON
	// body, statement Sid/bodies, or condition values. PR4b of #1134 emits this
	// fact; the resource-policy-aware CAN_PERFORM reducer follow-up consumes it.
	AWSResourcePolicyPermissionFactKind = "aws_resource_policy_permission"
	// AWSWarningFactKind identifies one non-fatal AWS scanner warning.
	AWSWarningFactKind = "aws_warning"

	// AWSResourceSchemaVersion is the first AWS resource fact schema.
	AWSResourceSchemaVersion = "1.0.0"
	// AWSRelationshipSchemaVersion is the first AWS relationship fact schema.
	AWSRelationshipSchemaVersion = "1.0.0"
	// AWSTagObservationSchemaVersion is the first AWS tag observation schema.
	AWSTagObservationSchemaVersion = "1.0.0"
	// AWSDNSRecordSchemaVersion is the first AWS DNS record schema.
	AWSDNSRecordSchemaVersion = "1.0.0"
	// AWSImageReferenceSchemaVersion is the first AWS image reference schema.
	AWSImageReferenceSchemaVersion = "1.0.0"
	// AWSSecurityGroupRuleSchemaVersion is the first AWS security-group-rule
	// posture fact schema.
	AWSSecurityGroupRuleSchemaVersion = "1.0.0"
	// AWSIAMPermissionSchemaVersion is the first derived IAM permission schema.
	AWSIAMPermissionSchemaVersion = "1.0.0"
	// AWSResourcePolicyPermissionSchemaVersion is the first derived
	// resource-policy permission schema.
	AWSResourcePolicyPermissionSchemaVersion = "1.0.0"
	// AWSWarningSchemaVersion is the first AWS warning fact schema.
	AWSWarningSchemaVersion = "1.0.0"
)

var awsFactKinds = []string{
	AWSResourceFactKind,
	AWSRelationshipFactKind,
	AWSTagObservationFactKind,
	AWSDNSRecordFactKind,
	AWSImageReferenceFactKind,
	AWSSecurityGroupRuleFactKind,
	AWSIAMPermissionFactKind,
	AWSResourcePolicyPermissionFactKind,
	AWSWarningFactKind,
}

var awsSchemaVersions = map[string]string{
	AWSResourceFactKind:                 AWSResourceSchemaVersion,
	AWSRelationshipFactKind:             AWSRelationshipSchemaVersion,
	AWSTagObservationFactKind:           AWSTagObservationSchemaVersion,
	AWSDNSRecordFactKind:                AWSDNSRecordSchemaVersion,
	AWSImageReferenceFactKind:           AWSImageReferenceSchemaVersion,
	AWSSecurityGroupRuleFactKind:        AWSSecurityGroupRuleSchemaVersion,
	AWSIAMPermissionFactKind:            AWSIAMPermissionSchemaVersion,
	AWSResourcePolicyPermissionFactKind: AWSResourcePolicyPermissionSchemaVersion,
	AWSWarningFactKind:                  AWSWarningSchemaVersion,
}

// AWSFactKinds returns the accepted AWS fact kinds in their emission order.
func AWSFactKinds() []string {
	return slices.Clone(awsFactKinds)
}

// AWSSchemaVersion returns the schema version for an AWS fact kind.
func AWSSchemaVersion(factKind string) (string, bool) {
	version, ok := awsSchemaVersions[factKind]
	return version, ok
}
