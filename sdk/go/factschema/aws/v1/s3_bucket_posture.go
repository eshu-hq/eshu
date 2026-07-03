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
}
