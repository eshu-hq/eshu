// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSynthetics identifies the regional Amazon CloudWatch Synthetics
	// metadata-only scan slice. The scanner reads canary control-plane metadata
	// through DescribeCanaries and never reads or persists canary script source
	// code, run artifacts (logs, screenshots, HAR files), or run results, and
	// never creates, updates, starts, stops, or deletes a canary.
	ServiceSynthetics = "synthetics"
)

const (
	// ResourceTypeSyntheticsCanary identifies an Amazon CloudWatch Synthetics
	// canary metadata resource. The scanner emits identity, runtime version,
	// status state, schedule expression and duration, retention, run resource
	// limits, and timeline timestamps only. Canary script source code and run
	// artifacts stay outside the contract.
	ResourceTypeSyntheticsCanary = "aws_synthetics_canary"
)

const (
	// RelationshipSyntheticsCanaryUsesS3Bucket records a Synthetics canary's
	// reported Amazon S3 artifact bucket dependency, where run artifacts (logs,
	// screenshots, HAR files) are stored. Synthetics reports an artifact
	// location path, so the scanner extracts the bucket name and keys the target
	// by the partition-aware bucket ARN the S3 scanner publishes.
	RelationshipSyntheticsCanaryUsesS3Bucket = "synthetics_canary_uses_s3_bucket"
	// RelationshipSyntheticsCanaryUsesIAMRole records a Synthetics canary's
	// execution IAM role dependency. AWS reports a role ARN, which matches how
	// the IAM scanner publishes its role resource_id.
	RelationshipSyntheticsCanaryUsesIAMRole = "synthetics_canary_uses_iam_role"
	// RelationshipSyntheticsCanaryUsesSubnet records a Synthetics canary's VPC
	// subnet dependency when the canary runs inside a VPC. The target is keyed by
	// the bare subnet id (subnet-...) the EC2 scanner publishes.
	RelationshipSyntheticsCanaryUsesSubnet = "synthetics_canary_uses_subnet"
	// RelationshipSyntheticsCanaryUsesSecurityGroup records a Synthetics canary's
	// VPC security group dependency when the canary runs inside a VPC. The target
	// is keyed by the bare security group id (sg-...) the EC2 scanner publishes.
	RelationshipSyntheticsCanaryUsesSecurityGroup = "synthetics_canary_uses_security_group"
)
