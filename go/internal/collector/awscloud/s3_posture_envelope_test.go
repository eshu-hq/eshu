// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func s3PostureBoundary(observedAt time.Time) Boundary {
	return Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         ServiceS3,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:s3:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          observedAt,
	}
}

func TestNewS3BucketPostureEnvelopeCarriesDerivedPosture(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC)
	envelope, err := NewS3BucketPostureEnvelope(S3BucketPostureObservation{
		Boundary:                    s3PostureBoundary(observedAt),
		BucketARN:                   "arn:aws:s3:::orders-artifacts",
		BucketName:                  "orders-artifacts",
		BlockPublicACLs:             boolPtr(true),
		IgnorePublicACLs:            boolPtr(true),
		BlockPublicPolicy:           boolPtr(true),
		RestrictPublicBuckets:       boolPtr(true),
		BlockPublicAccessAllEnabled: boolPtr(true),
		DefaultEncryptionEnabled:    true,
		EncryptionAlgorithms:        []string{"aws:kms"},
		SSEKMSKeyARN:                "arn:aws:kms:us-east-1:123456789012:key/orders",
		BucketKeyEnabled:            true,
		VersioningStatus:            "Enabled",
		VersioningEnabled:           true,
		MFADeleteEnabled:            false,
		ObjectOwnership:             []string{"BucketOwnerEnforced"},
		ACLDisabled:                 true,
		LoggingEnabled:              true,
		LoggingTargetBucket:         "orders-logs",
		ReplicationEnabled:          true,
		PolicyPresent:               true,
		PolicyGrantsPublic:          boolPtr(false),
		PolicyGrantsCrossAccount:    boolPtr(true),
	})
	if err != nil {
		t.Fatalf("NewS3BucketPostureEnvelope() error = %v, want nil", err)
	}

	if envelope.FactKind != facts.S3BucketPostureFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.S3BucketPostureFactKind)
	}
	if envelope.SchemaVersion != facts.S3BucketPostureSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.S3BucketPostureSchemaVersionV1)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.StableFactKey == "" {
		t.Fatalf("StableFactKey is empty")
	}

	p := envelope.Payload
	wantString := map[string]string{
		"account_id":            "123456789012",
		"region":                "us-east-1",
		"service_kind":          ServiceS3,
		"collector_instance_id": "aws-prod",
		"bucket_arn":            "arn:aws:s3:::orders-artifacts",
		"bucket_name":           "orders-artifacts",
		"sse_kms_key_arn":       "arn:aws:kms:us-east-1:123456789012:key/orders",
		"versioning_status":     "Enabled",
		"logging_target_bucket": "orders-logs",
	}
	for key, want := range wantString {
		if got, _ := p[key].(string); got != want {
			t.Fatalf("payload[%q] = %#v, want %q", key, p[key], want)
		}
	}

	wantBool := map[string]bool{
		"block_public_acls":           true,
		"ignore_public_acls":          true,
		"block_public_policy":         true,
		"restrict_public_buckets":     true,
		"block_public_access_all":     true,
		"default_encryption_enabled":  true,
		"bucket_key_enabled":          true,
		"versioning_enabled":          true,
		"mfa_delete_enabled":          false,
		"acl_disabled":                true,
		"logging_enabled":             true,
		"replication_enabled":         true,
		"policy_present":              true,
		"policy_grants_public":        false,
		"policy_grants_cross_account": true,
	}
	for key, want := range wantBool {
		if got, ok := p[key].(bool); !ok || got != want {
			t.Fatalf("payload[%q] = %#v, want bool %v", key, p[key], want)
		}
	}

	algorithms, ok := p["encryption_algorithms"].([]string)
	if !ok || len(algorithms) != 1 || algorithms[0] != "aws:kms" {
		t.Fatalf("payload[encryption_algorithms] = %#v, want [aws:kms]", p["encryption_algorithms"])
	}
	ownership, ok := p["object_ownership"].([]string)
	if !ok || len(ownership) != 1 || ownership[0] != "BucketOwnerEnforced" {
		t.Fatalf("payload[object_ownership] = %#v, want [BucketOwnerEnforced]", p["object_ownership"])
	}

	anchors, ok := p["correlation_anchors"].([]string)
	if !ok {
		t.Fatalf("payload[correlation_anchors] = %#v, want []string", p["correlation_anchors"])
	}
	if !containsString(anchors, "arn:aws:s3:::orders-artifacts") {
		t.Fatalf("correlation_anchors %v missing bucket ARN", anchors)
	}
}

// TestNewS3BucketPostureEnvelopeRejectsMissingIdentity proves the builder fails
// when neither bucket ARN nor name identifies the posture subject, so a posture
// fact can never be written without a join anchor.
func TestNewS3BucketPostureEnvelopeRejectsMissingIdentity(t *testing.T) {
	_, err := NewS3BucketPostureEnvelope(S3BucketPostureObservation{
		Boundary: s3PostureBoundary(time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC)),
	})
	if err == nil {
		t.Fatalf("NewS3BucketPostureEnvelope() error = nil, want missing-identity error")
	}
}

// TestNewS3BucketPostureEnvelopeNeverCarriesRawPolicy is a redaction guard: the
// posture fact must carry only derived booleans/identifiers, never the raw
// bucket policy document, ACL grants, or object data.
func TestNewS3BucketPostureEnvelopeNeverCarriesRawPolicy(t *testing.T) {
	envelope, err := NewS3BucketPostureEnvelope(S3BucketPostureObservation{
		Boundary:                 s3PostureBoundary(time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC)),
		BucketARN:                "arn:aws:s3:::orders-artifacts",
		BucketName:               "orders-artifacts",
		PolicyPresent:            true,
		PolicyGrantsPublic:       boolPtr(true),
		PolicyGrantsCrossAccount: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("NewS3BucketPostureEnvelope() error = %v, want nil", err)
	}
	for _, forbidden := range []string{
		"policy",
		"policy_json",
		"policy_document",
		"acl",
		"acl_grants",
		"statements",
		"objects",
		"object_keys",
	} {
		if _, exists := envelope.Payload[forbidden]; exists {
			t.Fatalf("payload carries forbidden key %q; posture fact must stay derived/metadata-only", forbidden)
		}
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
