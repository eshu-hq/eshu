// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewS3ExternalPrincipalGrantEnvelopeCarriesBoundedMetadata(t *testing.T) {
	envelope, err := NewS3ExternalPrincipalGrantEnvelope(S3ExternalPrincipalGrantObservation{
		Boundary:           s3PostureBoundary(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)),
		BucketARN:          "arn:aws:s3:::orders-artifacts",
		BucketName:         "orders-artifacts",
		PrincipalKind:      S3ExternalPrincipalKindAWSAccount,
		PrincipalValue:     "999988887777",
		PrincipalAccountID: "999988887777",
		GrantOutcome:       S3ExternalPrincipalGrantOutcomeCrossAccount,
	})
	if err != nil {
		t.Fatalf("NewS3ExternalPrincipalGrantEnvelope() error = %v, want nil", err)
	}

	if envelope.FactKind != facts.S3ExternalPrincipalGrantFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.S3ExternalPrincipalGrantFactKind)
	}
	if envelope.SchemaVersion != facts.S3ExternalPrincipalGrantSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.S3ExternalPrincipalGrantSchemaVersionV1)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}

	p := envelope.Payload
	wantString := map[string]string{
		"account_id":           "123456789012",
		"region":               "us-east-1",
		"service_kind":         ServiceS3,
		"bucket_arn":           "arn:aws:s3:::orders-artifacts",
		"bucket_name":          "orders-artifacts",
		"principal_kind":       S3ExternalPrincipalKindAWSAccount,
		"principal_value":      "999988887777",
		"principal_account_id": "999988887777",
		"grant_outcome":        S3ExternalPrincipalGrantOutcomeCrossAccount,
	}
	for key, want := range wantString {
		if got, _ := p[key].(string); got != want {
			t.Fatalf("payload[%q] = %#v, want %q", key, p[key], want)
		}
	}
	wantBool := map[string]bool{
		"is_public":            false,
		"is_cross_account":     true,
		"is_service_principal": false,
	}
	for key, want := range wantBool {
		if got, ok := p[key].(bool); !ok || got != want {
			t.Fatalf("payload[%q] = %#v, want bool %v", key, p[key], want)
		}
	}
}

func TestNewS3ExternalPrincipalGrantEnvelopeNeverCarriesRawPolicy(t *testing.T) {
	envelope, err := NewS3ExternalPrincipalGrantEnvelope(S3ExternalPrincipalGrantObservation{
		Boundary:       s3PostureBoundary(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)),
		BucketARN:      "arn:aws:s3:::orders-artifacts",
		BucketName:     "orders-artifacts",
		PrincipalKind:  S3ExternalPrincipalKindPublic,
		PrincipalValue: "*",
		GrantOutcome:   S3ExternalPrincipalGrantOutcomePublic,
	})
	if err != nil {
		t.Fatalf("NewS3ExternalPrincipalGrantEnvelope() error = %v, want nil", err)
	}
	for _, forbidden := range []string{
		"policy",
		"policy_json",
		"policy_document",
		"statement",
		"statements",
		"condition",
		"condition_values",
		"acl",
		"acl_grants",
		"objects",
		"object_keys",
	} {
		if _, exists := envelope.Payload[forbidden]; exists {
			t.Fatalf("payload carries forbidden key %q; grant fact must stay metadata-only", forbidden)
		}
	}
}

func TestNewS3ExternalPrincipalGrantEnvelopeRejectsMissingIdentity(t *testing.T) {
	_, err := NewS3ExternalPrincipalGrantEnvelope(S3ExternalPrincipalGrantObservation{
		Boundary:      s3PostureBoundary(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)),
		BucketARN:     "arn:aws:s3:::orders-artifacts",
		PrincipalKind: S3ExternalPrincipalKindAWSAccount,
		GrantOutcome:  S3ExternalPrincipalGrantOutcomeCrossAccount,
	})
	if err == nil {
		t.Fatalf("NewS3ExternalPrincipalGrantEnvelope() error = nil, want missing principal identity")
	}
}
