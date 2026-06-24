// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSecretsManager identifies the regional AWS Secrets Manager
	// metadata-only scan slice.
	ServiceSecretsManager = "secretsmanager"
)

const (
	// ResourceTypeSecretsManagerSecret identifies a Secrets Manager secret
	// metadata resource.
	ResourceTypeSecretsManagerSecret = "aws_secretsmanager_secret"
)

const (
	// RelationshipSecretsManagerSecretUsesKMSKey records a secret's reported
	// KMS key dependency.
	RelationshipSecretsManagerSecretUsesKMSKey = "secretsmanager_secret_uses_kms_key"
	// RelationshipSecretsManagerSecretUsesRotationLambda records a secret's
	// reported rotation Lambda dependency.
	RelationshipSecretsManagerSecretUsesRotationLambda = "secretsmanager_secret_uses_rotation_lambda"
)
