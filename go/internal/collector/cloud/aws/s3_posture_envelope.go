// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
	payload, err := factschema.EncodeS3BucketPosture(awsv1.S3BucketPosture{
		AccountID:                observation.Boundary.AccountID,
		Region:                   observation.Boundary.Region,
		ServiceKind:              boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:      boundaryValue(observation.Boundary.CollectorInstanceID),
		BucketARN:                stringValuePtr(bucketARN),
		BucketName:               stringValuePtr(bucketName),
		LoggingTargetBucket:      stringValuePtr(strings.TrimSpace(observation.LoggingTargetBucket)),
		PolicyPresent:            boolValuePtr(observation.PolicyPresent),
		PolicyGrantsPublic:       observation.PolicyGrantsPublic,
		BlockPublicAccessAll:     observation.BlockPublicAccessAllEnabled,
		IgnorePublicACLs:         observation.IgnorePublicACLs,
		RestrictPublicBuckets:    observation.RestrictPublicBuckets,
		BlockPublicACLs:          observation.BlockPublicACLs,
		BlockPublicPolicy:        observation.BlockPublicPolicy,
		DefaultEncryptionEnabled: boolValuePtr(observation.DefaultEncryptionEnabled),
		EncryptionAlgorithms:     cloneStrings(observation.EncryptionAlgorithms),
		SSEKMSKeyARN:             stringValuePtr(strings.TrimSpace(observation.SSEKMSKeyARN)),
		BucketKeyEnabled:         boolValuePtr(observation.BucketKeyEnabled),
		VersioningStatus:         stringValuePtr(strings.TrimSpace(observation.VersioningStatus)),
		VersioningEnabled:        boolValuePtr(observation.VersioningEnabled),
		MFADeleteEnabled:         boolValuePtr(observation.MFADeleteEnabled),
		ObjectOwnership:          cloneStrings(observation.ObjectOwnership),
		ACLDisabled:              boolValuePtr(observation.ACLDisabled),
		LoggingEnabled:           boolValuePtr(observation.LoggingEnabled),
		ReplicationEnabled:       boolValuePtr(observation.ReplicationEnabled),
		PolicyGrantsCrossAccount: observation.PolicyGrantsCrossAccount,
		CorrelationAnchors:       anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode s3_bucket_posture payload: %w", err)
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
