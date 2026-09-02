// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Join modes for edge target resolution shared by the AWS relationship edge
// projection (issue #805) and observability coverage correlation (issue #391).
// These are the closed enum the AWS design doc §5.2 documents and the
// join_mode / resolution_mode metric dimension carries in both families. They
// are bounded and stable so operators can group the edge-projection counter by
// mode.
const (
	// JoinModeARN resolves a target whose identity is an ARN (or ARN-shaped
	// resource_id), the common case for IAM/S3/KMS/MQ targets.
	JoinModeARN = "arn"
	// JoinModeBareID resolves a target whose identity is a bare AWS id such as
	// vpc-…, subnet-…, sg-…, igw-….
	JoinModeBareID = "bare_id"
	// JoinModeCorrelationAnchor resolves a name-only target (SageMaker
	// endpoint->config, MQ shared-configuration fallback, CloudFormation
	// stack-by-name) via the resource's published correlation anchors.
	JoinModeCorrelationAnchor = "correlation_anchor"
)
