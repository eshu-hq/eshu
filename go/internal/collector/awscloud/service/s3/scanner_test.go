// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsS3MetadataOnlyBucketFactsAndLoggingRelationships(t *testing.T) {
	created := time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)
	client := fakeClient{buckets: []Bucket{{
		Name:         "orders-artifacts",
		Region:       "us-east-1",
		CreationTime: created,
		Tags:         map[string]string{"Environment": "prod"},
		Versioning: Versioning{
			Status:    "Enabled",
			MFADelete: "Disabled",
		},
		Encryption: Encryption{Rules: []EncryptionRule{{
			Algorithm:      "aws:kms",
			KMSMasterKeyID: "arn:aws:kms:us-east-1:123456789012:key/orders",
			BucketKey:      true,
		}}},
		PublicAccessBlock: PublicAccessBlock{
			BlockPublicACLs:       boolPtr(true),
			IgnorePublicACLs:      boolPtr(true),
			BlockPublicPolicy:     boolPtr(true),
			RestrictPublicBuckets: boolPtr(true),
		},
		PolicyIsPublic:    boolPtr(false),
		OwnershipControls: []string{"BucketOwnerEnforced"},
		Website: Website{
			Enabled:               true,
			HasIndexDocument:      true,
			HasErrorDocument:      true,
			RedirectAllRequestsTo: "assets.example.com",
			RoutingRuleCount:      2,
		},
		Logging: Logging{
			Enabled:      true,
			TargetBucket: "orders-logs",
			TargetPrefix: "s3/",
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeS3Bucket)
	if got, want := resource.Payload["arn"], "arn:aws:s3:::orders-artifacts"; got != want {
		t.Fatalf("bucket arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["name"], "orders-artifacts"; got != want {
		t.Fatalf("bucket name = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["tags"], map[string]string{"Environment": "prod"}; !stringMapsEqual(got, want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "bucket_region", "us-east-1")
	assertAttribute(t, attributes, "creation_time", created)
	assertAttribute(t, attributes, "versioning_status", "Enabled")
	assertAttribute(t, attributes, "mfa_delete", "Disabled")
	assertAttribute(t, attributes, "default_encryption_algorithms", []string{"aws:kms"})
	assertAttribute(t, attributes, "kms_master_key_ids", []string{"arn:aws:kms:us-east-1:123456789012:key/orders"})
	assertAttribute(t, attributes, "bucket_key_enabled", true)
	assertAttribute(t, attributes, "block_public_acls", true)
	assertAttribute(t, attributes, "ignore_public_acls", true)
	assertAttribute(t, attributes, "block_public_policy", true)
	assertAttribute(t, attributes, "restrict_public_buckets", true)
	assertAttribute(t, attributes, "policy_is_public", false)
	assertAttribute(t, attributes, "ownership_controls", []string{"BucketOwnerEnforced"})
	assertAttribute(t, attributes, "website_enabled", true)
	assertAttribute(t, attributes, "website_has_index_document", true)
	assertAttribute(t, attributes, "website_has_error_document", true)
	assertAttribute(t, attributes, "website_redirect_host_name", "assets.example.com")
	assertAttribute(t, attributes, "website_routing_rule_count", 2)
	assertAttribute(t, attributes, "logging_enabled", true)
	assertAttribute(t, attributes, "logging_target_bucket", "orders-logs")
	assertAttribute(t, attributes, "logging_target_prefix", "s3/")
	for _, forbidden := range []string{
		"objects",
		"object_keys",
		"object_count",
		"policy",
		"policy_json",
		"acl_grants",
		"replication_rules",
		"lifecycle_rules",
		"notification_configuration",
		"inventory_configuration",
		"analytics_configuration",
		"metrics_configuration",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; S3 scanner must stay metadata-only", forbidden)
		}
	}

	relationship := relationshipByType(t, envelopes, awscloud.RelationshipS3BucketLogsToBucket)
	if got, want := relationship.Payload["source_arn"], "arn:aws:s3:::orders-artifacts"; got != want {
		t.Fatalf("logging relationship source_arn = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_arn"], "arn:aws:s3:::orders-logs"; got != want {
		t.Fatalf("logging relationship target_arn = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("logging relationship target_type = %#v, want %q", got, want)
	}
	relationshipAttributes := attributesOf(t, relationship)
	assertAttribute(t, relationshipAttributes, "target_prefix", "s3/")
}

func TestScannerEmitsDerivedBucketPostureFact(t *testing.T) {
	created := time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)
	client := fakeClient{buckets: []Bucket{{
		Name:         "orders-artifacts",
		Region:       "us-east-1",
		CreationTime: created,
		Versioning: Versioning{
			Status:    "Enabled",
			MFADelete: "Enabled",
		},
		Encryption: Encryption{Rules: []EncryptionRule{{
			Algorithm:      "aws:kms",
			KMSMasterKeyID: "arn:aws:kms:us-east-1:123456789012:key/orders",
			BucketKey:      true,
		}}},
		PublicAccessBlock: PublicAccessBlock{
			BlockPublicACLs:       boolPtr(true),
			IgnorePublicACLs:      boolPtr(true),
			BlockPublicPolicy:     boolPtr(true),
			RestrictPublicBuckets: boolPtr(true),
		},
		PolicyIsPublic:           boolPtr(false),
		PolicyPresent:            true,
		PolicyGrantsPublic:       boolPtr(false),
		PolicyGrantsCrossAccount: boolPtr(true),
		OwnershipControls:        []string{"BucketOwnerEnforced"},
		Replication:              Replication{Enabled: true},
		Logging: Logging{
			Enabled:      true,
			TargetBucket: "orders-logs",
			TargetPrefix: "s3/",
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	posture := postureByBucket(t, envelopes, "arn:aws:s3:::orders-artifacts")
	wantString := map[string]string{
		"bucket_arn":            "arn:aws:s3:::orders-artifacts",
		"bucket_name":           "orders-artifacts",
		"sse_kms_key_arn":       "arn:aws:kms:us-east-1:123456789012:key/orders",
		"versioning_status":     "Enabled",
		"logging_target_bucket": "orders-logs",
	}
	for key, want := range wantString {
		if got, _ := posture.Payload[key].(string); got != want {
			t.Fatalf("posture[%q] = %#v, want %q", key, posture.Payload[key], want)
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
		"mfa_delete_enabled":          true,
		"acl_disabled":                true,
		"logging_enabled":             true,
		"replication_enabled":         true,
		"policy_present":              true,
		"policy_grants_public":        false,
		"policy_grants_cross_account": true,
	}
	for key, want := range wantBool {
		got, ok := posture.Payload[key].(bool)
		if !ok || got != want {
			t.Fatalf("posture[%q] = %#v, want bool %v", key, posture.Payload[key], want)
		}
	}
	for _, forbidden := range []string{
		"policy",
		"policy_json",
		"policy_document",
		"acl_grants",
		"statements",
		"objects",
		"replication_rules",
	} {
		if _, exists := posture.Payload[forbidden]; exists {
			t.Fatalf("posture carries forbidden key %q; posture fact must stay derived/metadata-only", forbidden)
		}
	}
}

func TestScannerEmitsExternalPrincipalGrantFacts(t *testing.T) {
	client := fakeClient{buckets: []Bucket{{
		Name: "orders-artifacts",
		ExternalPrincipalGrants: []ExternalPrincipalGrant{
			{
				PrincipalKind:      awscloud.S3ExternalPrincipalKindAWSAccount,
				PrincipalValue:     "999988887777",
				PrincipalAccountID: "999988887777",
				GrantOutcome:       awscloud.S3ExternalPrincipalGrantOutcomeCrossAccount,
				CrossAccount:       true,
			},
			{
				PrincipalKind:     awscloud.S3ExternalPrincipalKindPublic,
				PrincipalValue:    "*",
				GrantOutcome:      awscloud.S3ExternalPrincipalGrantOutcomePublic,
				Public:            true,
				SourceStatementID: "PublicRead",
			},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	accountGrant := externalPrincipalGrantByPrincipal(t, envelopes, awscloud.S3ExternalPrincipalKindAWSAccount, "999988887777")
	if got, want := accountGrant.Payload["bucket_arn"], "arn:aws:s3:::orders-artifacts"; got != want {
		t.Fatalf("account grant bucket_arn = %#v, want %q", got, want)
	}
	if got, want := accountGrant.Payload["grant_outcome"], awscloud.S3ExternalPrincipalGrantOutcomeCrossAccount; got != want {
		t.Fatalf("account grant outcome = %#v, want %q", got, want)
	}
	if got, want := accountGrant.Payload["is_cross_account"], true; got != want {
		t.Fatalf("account grant is_cross_account = %#v, want %v", got, want)
	}

	publicGrant := externalPrincipalGrantByPrincipal(t, envelopes, awscloud.S3ExternalPrincipalKindPublic, "*")
	if got, want := publicGrant.Payload["grant_outcome"], awscloud.S3ExternalPrincipalGrantOutcomePublic; got != want {
		t.Fatalf("public grant outcome = %#v, want %q", got, want)
	}
	if got, want := publicGrant.Payload["source_statement_id"], "PublicRead"; got != want {
		t.Fatalf("public grant source_statement_id = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{
		"policy",
		"policy_json",
		"policy_document",
		"statement",
		"condition",
		"acl_grants",
		"object_keys",
	} {
		if _, exists := publicGrant.Payload[forbidden]; exists {
			t.Fatalf("external principal fact carries forbidden key %q", forbidden)
		}
	}
}

// TestScannerPostureDerivesPartition pins that the posture fact's bucket
// identity inherits the boundary partition, matching the resource node so the
// PR2 reducer join resolves in GovCloud and China.
func TestScannerPostureDerivesPartition(t *testing.T) {
	cases := []struct {
		name    string
		region  string
		wantARN string
	}{
		{name: "commercial", region: "us-east-1", wantARN: "arn:aws:s3:::my-bucket"},
		{name: "govcloud", region: "us-gov-west-1", wantARN: "arn:aws-us-gov:s3:::my-bucket"},
		{name: "china", region: "cn-north-1", wantARN: "arn:aws-cn:s3:::my-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := testBoundary()
			boundary.Region = tc.region
			client := fakeClient{buckets: []Bucket{{Name: "my-bucket"}}}
			envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
			if err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}
			posture := postureByBucket(t, envelopes, tc.wantARN)
			if got, _ := posture.Payload["bucket_arn"].(string); got != tc.wantARN {
				t.Fatalf("posture bucket_arn = %q, want %q", got, tc.wantARN)
			}
		})
	}
}

func TestScannerSkipsLoggingRelationshipWithoutTargetBucket(t *testing.T) {
	client := fakeClient{buckets: []Bucket{{
		Name:   "orders-artifacts",
		Region: "us-east-1",
		Logging: Logging{
			Enabled: true,
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipS3BucketLogsToBucket); got != 0 {
		t.Fatalf("logging relationship count = %d, want 0 without target bucket", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceS3,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:s3:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	buckets []Bucket
}

func (c fakeClient) ListBuckets(context.Context) ([]Bucket, error) {
	return c.buckets, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func postureByBucket(t *testing.T, envelopes []facts.Envelope, bucketARN string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.S3BucketPostureFactKind {
			continue
		}
		if got, _ := envelope.Payload["bucket_arn"].(string); got == bucketARN {
			return envelope
		}
	}
	t.Fatalf("missing s3_bucket_posture for %q in %#v", bucketARN, envelopes)
	return facts.Envelope{}
}

func externalPrincipalGrantByPrincipal(t *testing.T, envelopes []facts.Envelope, kind, value string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.S3ExternalPrincipalGrantFactKind {
			continue
		}
		if gotKind, _ := envelope.Payload["principal_kind"].(string); gotKind != kind {
			continue
		}
		if gotValue, _ := envelope.Payload["principal_value"].(string); gotValue == value {
			return envelope
		}
	}
	t.Fatalf("missing s3_external_principal_grant kind=%q value=%q in %#v", kind, value, envelopes)
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotStrings, ok := got.([]string)
		if !ok || len(gotStrings) != len(want) {
			return false
		}
		for i := range want {
			if gotStrings[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}

func stringMapsEqual(got any, want map[string]string) bool {
	gotMap, ok := got.(map[string]string)
	if !ok || len(gotMap) != len(want) {
		return false
	}
	for key, value := range want {
		if gotMap[key] != value {
			return false
		}
	}
	return true
}

func boolPtr(value bool) *bool {
	return &value
}
