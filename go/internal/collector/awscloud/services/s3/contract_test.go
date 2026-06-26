// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/contracttest"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func s3Contract() contracttest.Contract {
	return contracttest.Contract{
		CollectorKind: awscloud.CollectorKind,
		FactKinds: []contracttest.FactKindShape{
			{
				Kind: facts.AWSResourceFactKind,
				RequiredPayloadKeys: []string{
					"account_id", "region", "service_kind", "collector_instance_id",
					"arn", "resource_id", "resource_type", "name", "state", "tags",
					"attributes", "correlation_anchors",
				},
			},
			{
				Kind: facts.AWSRelationshipFactKind,
				RequiredPayloadKeys: []string{
					"account_id", "region", "service_kind", "collector_instance_id",
					"relationship_type", "source_resource_id", "source_arn",
					"target_resource_id", "target_arn", "target_type", "attributes",
				},
			},
			{
				Kind: facts.S3BucketPostureFactKind,
				RequiredPayloadKeys: []string{
					"account_id", "region", "service_kind", "collector_instance_id",
					"bucket_arn", "bucket_name",
				},
			},
			{
				Kind: facts.S3ExternalPrincipalGrantFactKind,
				RequiredPayloadKeys: []string{
					"account_id", "region", "service_kind", "collector_instance_id",
					"bucket_arn", "bucket_name", "principal_kind", "principal_value", "grant_outcome",
				},
			},
			{
				Kind: facts.AWSResourcePolicyPermissionFactKind,
				RequiredPayloadKeys: []string{
					"account_id", "region", "service_kind", "collector_instance_id",
					"resource_arn", "resource_type", "effect", "actions",
				},
			},
		},
	}
}

// TestContractShape verifies that the S3 scanner output satisfies the
// per-collector fact-shape contract declared in
// specs/collector_fact_contract.v1.yaml. It exercises the reusable
// contracttest helpers on real scanner output.
func TestContractShape(t *testing.T) {
	client := fakeClient{buckets: []Bucket{{
		Name:   "orders-artifacts",
		Region: "us-east-1",
		Logging: Logging{
			Enabled:      true,
			TargetBucket: "orders-logs",
			TargetPrefix: "s3/",
		},
		Encryption: Encryption{Rules: []EncryptionRule{{
			Algorithm:      "aws:kms",
			KMSMasterKeyID: "arn:aws:kms:us-east-1:123456789012:key/orders",
		}}},
		ExternalPrincipalGrants: []ExternalPrincipalGrant{{
			PrincipalKind:  awscloud.S3ExternalPrincipalKindPublic,
			PrincipalValue: "*",
			GrantOutcome:   awscloud.S3ExternalPrincipalGrantOutcomePublic,
			Public:         true,
		}},
		ResourcePolicyStatements: []ResourcePolicyStatement{{
			StatementSID: "AllowPartner",
			Effect:       "Allow",
			Actions:      []string{"s3:GetObject"},
			Resources:    []string{"arn:aws:s3:::orders-artifacts/*"},
			PrincipalTypes: []string{"AWS"},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(t.Context(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	contracttest.AssertFactShape(t, s3Contract(), envelopes)
	contracttest.ValidateCollectorKind(t, s3Contract(), envelopes)

	t.Logf("contract shape verified: %d envelopes, fact kinds: %v",
		len(envelopes), contracttest.EnvelopeCounts(envelopes))
}

// TestContractRejectsMismatchedServiceKind exercises the shared
// service-kind rejection helper on the S3 scanner zero value.
func TestContractRejectsMismatchedServiceKind(t *testing.T) {
	contracttest.AssertRejectsMismatchedServiceKind(
		t,
		func(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
			return (Scanner{Client: fakeClient{}}).Scan(ctx, boundary)
		},
		testBoundary(),
		awscloud.ServiceSNS,
	)
}

// TestContractRequiresClient exercises the shared client-required helper on
// the S3 scanner without a client set.
func TestContractRequiresClient(t *testing.T) {
	contracttest.AssertRequiresClient(
		t,
		func(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
			return (Scanner{}).Scan(ctx, boundary)
		},
		testBoundary(),
	)
}
