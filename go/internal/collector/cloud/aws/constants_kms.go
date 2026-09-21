// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceKMS identifies the regional AWS Key Management Service
	// metadata-only scan slice. The scanner never invokes cryptographic
	// operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify, ReEncrypt,
	// GenerateMac, VerifyMac, GenerateDataKeyPair,
	// GenerateDataKeyWithoutPlaintext) and never invokes key lifecycle
	// mutation APIs (CreateKey, ScheduleKeyDeletion, CancelKeyDeletion,
	// EnableKey, DisableKey, PutKeyPolicy, CreateGrant, RevokeGrant,
	// RetireGrant, ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial).
	ServiceKMS = "kms"
)

const (
	// ResourceTypeKMSKey identifies a KMS customer master key metadata
	// resource. The scanner emits identity, usage, origin, manager, key
	// state, rotation status, and policy revision metadata only; key policy
	// Statement bodies and key material are never persisted.
	ResourceTypeKMSKey = "aws_kms_key"
	// ResourceTypeKMSAlias identifies a KMS alias metadata resource. Aliases
	// are name-only pointers; no key material or policy text is carried.
	ResourceTypeKMSAlias = "aws_kms_alias"
	// ResourceTypeKMSGrant identifies a KMS grant metadata resource. The
	// scanner emits grant identity, grantee principal, retiring principal,
	// and the bounded list of allowed operations. Grant encryption contexts
	// are never persisted.
	ResourceTypeKMSGrant = "aws_kms_grant"
)

const (
	// RelationshipKMSAliasTargetsKey records a KMS alias pointing at its
	// target key.
	RelationshipKMSAliasTargetsKey = "kms_alias_targets_key"
	// RelationshipKMSGrantOnKey records a KMS grant attached to its target
	// key.
	RelationshipKMSGrantOnKey = "kms_grant_on_key"
	// RelationshipKMSGrantForGrantee records the grantee principal reported
	// by a KMS grant. The principal is either an IAM ARN or an AWS service
	// principal and is emitted with the IAM principal scheme (target identity
	// "<type>:<principal>", target_arn set only for ARN-shaped values); trust
	// evaluation belongs to reducers, not this scanner.
	RelationshipKMSGrantForGrantee = "kms_grant_for_grantee"
)
