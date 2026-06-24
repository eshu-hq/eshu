// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSSM identifies the regional AWS Systems Manager Parameter Store
	// metadata-only scan slice.
	ServiceSSM = "ssm"
)

const (
	// ResourceTypeSSMParameter identifies an SSM Parameter Store metadata
	// resource.
	ResourceTypeSSMParameter = "aws_ssm_parameter"
)

const (
	// RelationshipSSMParameterUsesKMSKey records an SSM SecureString
	// parameter's reported KMS key dependency.
	RelationshipSSMParameterUsesKMSKey = "ssm_parameter_uses_kms_key"
)
