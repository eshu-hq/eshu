// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// S3BucketPostureObservation describes the derived security posture of one S3
// bucket. Every field is metadata-only: block-public-access flags,
// default-encryption detail (the SSE-KMS key ARN/id and bucket-key state),
// versioning and MFA-delete state, object-ownership / ACL-disabled state,
// access-logging target, replication presence, and booleans DERIVED from the
// bucket policy document. It never carries the raw bucket policy JSON, ACL
// grants, or object data.
//
// Pointer booleans distinguish an unknown setting (nil, the control-plane read
// returned no value) from an observed false. Plain booleans are scanner-derived
// summaries that default to false when the underlying configuration is absent.
type S3BucketPostureObservation struct {
	Boundary   Boundary
	BucketARN  string
	BucketName string

	// Block Public Access. Nil means the bucket had no public-access-block
	// configuration for that flag.
	BlockPublicACLs             *bool
	IgnorePublicACLs            *bool
	BlockPublicPolicy           *bool
	RestrictPublicBuckets       *bool
	BlockPublicAccessAllEnabled *bool

	// Default encryption detail.
	DefaultEncryptionEnabled bool
	EncryptionAlgorithms     []string
	SSEKMSKeyARN             string
	BucketKeyEnabled         bool

	// Versioning and MFA delete.
	VersioningStatus  string
	VersioningEnabled bool
	MFADeleteEnabled  bool

	// Object ownership / ACL state.
	ObjectOwnership []string
	ACLDisabled     bool

	// Access logging target.
	LoggingEnabled      bool
	LoggingTargetBucket string

	// Replication presence (no rule detail).
	ReplicationEnabled bool

	// Policy-derived booleans only. PolicyGrantsPublic / PolicyGrantsCrossAccount
	// are nil when no policy is present or the grant could not be derived.
	PolicyPresent            bool
	PolicyGrantsPublic       *bool
	PolicyGrantsCrossAccount *bool

	SourceURI      string
	SourceRecordID string
}

// NewS3BucketPostureEnvelope builds the durable s3_bucket_posture fact for one
// S3 bucket's derived security posture. It requires the bucket ARN or name as a
// join anchor and copies only derived booleans and safe identifiers into the
// payload; the raw bucket policy document never reaches this builder.
func NewS3BucketPostureEnvelope(observation S3BucketPostureObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	bucketARN := strings.TrimSpace(observation.BucketARN)
	bucketName := strings.TrimSpace(observation.BucketName)
	if bucketARN == "" && bucketName == "" {
		return facts.Envelope{}, fmt.Errorf("s3 bucket posture observation requires bucket_arn or bucket_name")
	}
	identity := bucketARN
	if identity == "" {
		identity = bucketName
	}
	stableKey := facts.StableID(facts.S3BucketPostureFactKind, map[string]any{
		"account_id": observation.Boundary.AccountID,
		"bucket":     identity,
		"region":     observation.Boundary.Region,
	})
	anchors := normalizedAnchors(nil, bucketARN, bucketName, s3PostureURI(bucketName))
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"bucket_arn":            bucketARN,
		"bucket_name":           bucketName,

		"block_public_acls":       boolOrNil(observation.BlockPublicACLs),
		"ignore_public_acls":      boolOrNil(observation.IgnorePublicACLs),
		"block_public_policy":     boolOrNil(observation.BlockPublicPolicy),
		"restrict_public_buckets": boolOrNil(observation.RestrictPublicBuckets),
		"block_public_access_all": boolOrNil(observation.BlockPublicAccessAllEnabled),

		"default_encryption_enabled": observation.DefaultEncryptionEnabled,
		"encryption_algorithms":      cloneStrings(observation.EncryptionAlgorithms),
		"sse_kms_key_arn":            strings.TrimSpace(observation.SSEKMSKeyARN),
		"bucket_key_enabled":         observation.BucketKeyEnabled,

		"versioning_status":  strings.TrimSpace(observation.VersioningStatus),
		"versioning_enabled": observation.VersioningEnabled,
		"mfa_delete_enabled": observation.MFADeleteEnabled,

		"object_ownership": cloneStrings(observation.ObjectOwnership),
		"acl_disabled":     observation.ACLDisabled,

		"logging_enabled":       observation.LoggingEnabled,
		"logging_target_bucket": strings.TrimSpace(observation.LoggingTargetBucket),

		"replication_enabled": observation.ReplicationEnabled,

		"policy_present":              observation.PolicyPresent,
		"policy_grants_public":        boolOrNil(observation.PolicyGrantsPublic),
		"policy_grants_cross_account": boolOrNil(observation.PolicyGrantsCrossAccount),

		"correlation_anchors": anchors,
	}
	return newEnvelope(
		observation.Boundary,
		facts.S3BucketPostureFactKind,
		facts.S3BucketPostureSchemaVersionV1,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity+"#posture"),
		observation.SourceURI,
		payload,
	), nil
}

// s3PostureURI returns the s3:// correlation URI for a bucket name, or empty
// when the name is blank.
func s3PostureURI(bucketName string) string {
	bucketName = strings.TrimSpace(bucketName)
	if bucketName == "" {
		return ""
	}
	return "s3://" + bucketName
}

// cloneStrings returns a trimmed, non-empty copy of input, or nil when input is
// empty so posture payloads carry stable string slices.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
