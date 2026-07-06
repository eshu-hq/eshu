// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// S3BucketPosture is the schema-version-1 typed payload for the
// "s3_bucket_posture" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Only AccountID and Region are required: the collector emitter
// (awscloud.NewS3BucketPostureEnvelope) validates bucket_arn OR bucket_name
// non-empty as an either-or identity, so NEITHER BucketARN nor BucketName can be
// required on its own — requiring one would dead-letter a valid fact identified
// only by the other. The reducer's source-bucket-name derivation already
// tolerates a blank BucketName by falling back to the bucket_arn tail. All other
// fields are optional posture properties: LoggingTargetBucket is blank when
// logging is disabled (the normal no-edge state), and the public-access booleans
// are pointers so an unreported flag (the control-plane read returned no value)
// stays distinct from an observed false.
//
// This struct models only the posture fields the in-scope reducer consumers
// read (the LOGS_TO join and the S3 internet-exposure derivation). The
// collector emits additional posture fields (encryption detail, versioning,
// object ownership, replication) that no migrated handler decodes; they remain
// valid payload keys and are simply not surfaced as struct fields yet, which the
// decode seam permits because unknown keys are ignored on unmarshal.
type S3BucketPosture struct {
	// AccountID is the AWS account the bucket was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the bucket was observed in. Required.
	Region string `json:"region"`

	// ServiceKind is the collector service-kind boundary token. Optional.
	ServiceKind *string `json:"service_kind,omitempty"`

	// CollectorInstanceID is the collector instance boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// BucketARN is the bucket ARN. Optional: the emitter requires bucket_arn OR
	// bucket_name, so this may be empty when only the name was observed; the
	// reducer derives the name from the ARN tail when BucketName is blank.
	BucketARN *string `json:"bucket_arn,omitempty"`

	// BucketName is the bare bucket name. Optional: the emitter requires
	// bucket_arn OR bucket_name, so this may be empty when only the ARN was
	// observed.
	BucketName *string `json:"bucket_name,omitempty"`

	// LoggingTargetBucket is the bucket that receives this bucket's access
	// logs. Optional: blank when access logging is disabled — the normal
	// no-LOGS_TO-edge state, not a malformed fact.
	LoggingTargetBucket *string `json:"logging_target_bucket,omitempty"`

	// PolicyPresent reports whether the bucket has an attached policy. Optional.
	PolicyPresent *bool `json:"policy_present,omitempty"`

	// PolicyGrantsPublic is the collector-derived flag marking a bucket policy
	// that grants public access. Optional pointer so nil (no policy or grant
	// not derivable) stays distinct from an observed false.
	PolicyGrantsPublic *bool `json:"policy_grants_public,omitempty"`

	// BlockPublicAccessAll reports whether the account/bucket block-public-
	// access "all" flag is enabled. Optional pointer preserving unreported vs
	// observed false.
	BlockPublicAccessAll *bool `json:"block_public_access_all,omitempty"`

	// IgnorePublicACLs reports the ignore-public-ACLs block-public-access flag.
	// Optional pointer preserving unreported vs observed false.
	IgnorePublicACLs *bool `json:"ignore_public_acls,omitempty"`

	// RestrictPublicBuckets reports the restrict-public-buckets block-public-
	// access flag. Optional pointer preserving unreported vs observed false.
	RestrictPublicBuckets *bool `json:"restrict_public_buckets,omitempty"`

	// BlockPublicACLs reports the block-public-ACLs flag. Optional.
	BlockPublicACLs *bool `json:"block_public_acls,omitempty"`

	// BlockPublicPolicy reports the block-public-policy flag. Optional.
	BlockPublicPolicy *bool `json:"block_public_policy,omitempty"`

	// DefaultEncryptionEnabled reports whether default encryption is enabled.
	DefaultEncryptionEnabled *bool `json:"default_encryption_enabled,omitempty"`

	// EncryptionAlgorithms is the normalized default-encryption algorithm list.
	EncryptionAlgorithms []string `json:"encryption_algorithms,omitempty"`

	// SSEKMSKeyARN is the default SSE-KMS key ARN, when configured.
	SSEKMSKeyARN *string `json:"sse_kms_key_arn,omitempty"`

	// BucketKeyEnabled reports whether the S3 bucket key is enabled.
	BucketKeyEnabled *bool `json:"bucket_key_enabled,omitempty"`

	// VersioningStatus is the S3 versioning status string.
	VersioningStatus *string `json:"versioning_status,omitempty"`

	// VersioningEnabled reports whether bucket versioning is enabled.
	VersioningEnabled *bool `json:"versioning_enabled,omitempty"`

	// MFADeleteEnabled reports whether MFA delete is enabled.
	MFADeleteEnabled *bool `json:"mfa_delete_enabled,omitempty"`

	// ObjectOwnership carries observed object-ownership modes.
	ObjectOwnership []string `json:"object_ownership,omitempty"`

	// ACLDisabled reports whether ACLs are disabled by object ownership.
	ACLDisabled *bool `json:"acl_disabled,omitempty"`

	// LoggingEnabled reports whether access logging is enabled.
	LoggingEnabled *bool `json:"logging_enabled,omitempty"`

	// ReplicationEnabled reports whether replication is enabled.
	ReplicationEnabled *bool `json:"replication_enabled,omitempty"`

	// PolicyGrantsCrossAccount marks a policy with cross-account grants.
	PolicyGrantsCrossAccount *bool `json:"policy_grants_cross_account,omitempty"`

	// CorrelationAnchors are redaction-safe bucket anchors.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
