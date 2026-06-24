// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securitylake

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testDataLakeARN   = "arn:aws:securitylake:us-east-1:123456789012:data-lake/default"
	testS3BucketARN   = "arn:aws:s3:::aws-security-data-lake-us-east-1-abcdef"
	testKMSARN        = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testSubscriberARN = "arn:aws:securitylake:us-east-1:123456789012:subscriber/11111111-2222-3333-4444-555555555555"
	testSubRoleARN    = "arn:aws:iam::123456789012:role/AmazonSecurityLake-subscriber"
	testProviderRole  = "arn:aws:iam::123456789012:role/SecurityLakeCustomSource"
	testSubBucketARN  = "arn:aws:s3:::subscriber-delivery-bucket"
)

func TestScannerEmitsSecurityLakeMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		DataLakes: []DataLake{{
			ARN:                testDataLakeARN,
			Region:             "us-east-1",
			S3BucketARN:        testS3BucketARN,
			KMSKeyID:           testKMSARN,
			CreateStatus:       "COMPLETED",
			ExpirationDays:     365,
			TransitionCount:    2,
			ReplicationRegions: []string{"us-west-2"},
		}},
		LogSources: []LogSource{
			{Account: "123456789012", Region: "us-east-1", SourceName: "ROUTE53", SourceVersion: "1.0"},
			{
				Account: "123456789012", Region: "us-east-1",
				SourceName: "MyCustomSource", Custom: true, ProviderRoleARN: testProviderRole,
			},
		},
		Subscribers: []Subscriber{{
			ARN:              testSubscriberARN,
			ID:               "11111111-2222-3333-4444-555555555555",
			Name:             "analytics-team",
			Status:           "ACTIVE",
			AccessTypes:      []string{"S3", "LAKEFORMATION"},
			PrincipalAccount: "210987654321",
			RoleARN:          testSubRoleARN,
			S3BucketARN:      testSubBucketARN,
			SourceNames:      []string{"ROUTE53"},
			CreatedAt:        time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Data lake resource node.
	lake := resourceByType(t, envelopes, awscloud.ResourceTypeSecurityLakeDataLake)
	if got, want := lake.Payload["resource_id"], testDataLakeARN; got != want {
		t.Fatalf("data lake resource_id = %#v, want %q", got, want)
	}
	lakeAttrs := attributesOf(t, lake)
	assertAttribute(t, lakeAttrs, "create_status", "COMPLETED")
	assertAttribute(t, lakeAttrs, "s3_bucket_arn", testS3BucketARN)
	assertAttribute(t, lakeAttrs, "expiration_days", int32(365))

	// Subscriber resource node.
	subscriber := resourceByType(t, envelopes, awscloud.ResourceTypeSecurityLakeSubscriber)
	if got, want := subscriber.Payload["resource_id"], testSubscriberARN; got != want {
		t.Fatalf("subscriber resource_id = %#v, want %q", got, want)
	}
	subAttrs := attributesOf(t, subscriber)
	assertAttribute(t, subAttrs, "principal_account", "210987654321")
	assertAttribute(t, subAttrs, "access_types", []string{"S3", "LAKEFORMATION"})

	// data lake -> S3 bucket edge, keyed by the bucket ARN the S3 scanner publishes.
	lakeS3 := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeDataLakeUsesS3Bucket)
	assertEdgeTarget(t, lakeS3, awscloud.ResourceTypeS3Bucket, testS3BucketARN)

	// data lake -> KMS key edge.
	lakeKMS := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeDataLakeUsesKMSKey)
	assertEdgeTarget(t, lakeKMS, awscloud.ResourceTypeKMSKey, testKMSARN)

	// data lake -> Lake Formation registered resource edge, keyed by the bucket ARN.
	lakeLF := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeDataLakeRegisteredInLakeFormation)
	assertEdgeTarget(t, lakeLF, awscloud.ResourceTypeLakeFormationResource, testS3BucketARN)

	// log source -> data lake membership edge, keyed by the data lake ARN.
	srcInLake := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeLogSourceInDataLake)
	assertEdgeTarget(t, srcInLake, awscloud.ResourceTypeSecurityLakeDataLake, testDataLakeARN)

	// custom log source -> IAM role edge.
	srcRole := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeLogSourceUsesIAMRole)
	assertEdgeTarget(t, srcRole, awscloud.ResourceTypeIAMRole, testProviderRole)

	// subscriber -> IAM role edge.
	subRole := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeSubscriberUsesIAMRole)
	assertEdgeTarget(t, subRole, awscloud.ResourceTypeIAMRole, testSubRoleARN)
	if got, want := subRole.Payload["source_resource_id"], testSubscriberARN; got != want {
		t.Fatalf("subscriber->role source_resource_id = %#v, want %q", got, want)
	}

	// subscriber -> S3 bucket edge.
	subS3 := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeSubscriberUsesS3Bucket)
	assertEdgeTarget(t, subS3, awscloud.ResourceTypeS3Bucket, testSubBucketARN)

	// No subscriber credential / external id / endpoint leakage.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"external_id", "externalId", "subscriber_endpoint", "endpoint",
			"credentials", "access_token", "records", "log_records", "objects",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Security Lake scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsEdgesWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{DataLakes: []DataLake{{
		ARN:    testDataLakeARN,
		Region: "us-east-1",
		// No bucket, no resolvable KMS key: no S3, KMS, or Lake Formation edge.
		KMSKeyID: "S3_MANAGED",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsSubscriberRoleEdgeForNonARN(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Subscribers: []Subscriber{{
		ARN:     testSubscriberARN,
		ID:      "sub-1",
		Name:    "team",
		RoleARN: "not-an-arn",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipSecurityLakeSubscriberUsesIAMRole {
			t.Fatalf("emitted IAM role edge for non-ARN role identifier; expected skip")
		}
	}
	// The subscriber node still records the raw role value for visibility.
	subscriber := resourceByType(t, envelopes, awscloud.ResourceTypeSecurityLakeSubscriber)
	assertAttribute(t, attributesOf(t, subscriber), "role_arn", "not-an-arn")
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	lake := DataLake{ARN: testDataLakeARN, Region: "us-east-1", S3BucketARN: testS3BucketARN, KMSKeyID: testKMSARN}
	dataLakeID := dataLakeResourceID(lake)
	customSource := LogSource{
		Account: "123456789012", Region: "us-east-1",
		SourceName: "Custom", Custom: true, ProviderRoleARN: testProviderRole,
	}
	awsSource := LogSource{Account: "123456789012", Region: "us-east-1", SourceName: "ROUTE53"}
	subscriber := Subscriber{ARN: testSubscriberARN, ID: "s", RoleARN: testSubRoleARN, S3BucketARN: testSubBucketARN}

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		dataLakeS3Relationship(boundary, lake),
		dataLakeKMSRelationship(boundary, lake),
		dataLakeLakeFormationRelationship(boundary, lake),
		logSourceInDataLakeRelationship(boundary, dataLakeID, awsSource),
		logSourceIAMRoleRelationship(boundary, customSource),
		subscriberIAMRoleRelationship(boundary, subscriber),
		subscriberS3Relationship(boundary, subscriber),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerSynthesizesPartitionAwareIdentities(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govLakeARN := "arn:aws-us-gov:securitylake:us-gov-west-1:123456789012:data-lake/default"
	govBucketARN := "arn:aws-us-gov:s3:::gov-security-lake-bucket"
	client := fakeClient{snapshot: Snapshot{DataLakes: []DataLake{{
		ARN:         govLakeARN,
		Region:      "us-gov-west-1",
		S3BucketARN: govBucketARN,
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	lakeS3 := relationshipByType(t, envelopes, awscloud.RelationshipSecurityLakeDataLakeUsesS3Bucket)
	if got := lakeS3.Payload["target_resource_id"]; got != govBucketARN {
		t.Fatalf("GovCloud data lake->s3 target_resource_id = %#v, want %q", got, govBucketARN)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		DataLakes: []DataLake{{ARN: testDataLakeARN, Region: "us-east-1"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Security Lake ListSubscribers throttled after SDK retries; subscriber metadata omitted",
			SourceRecordID: "securitylake_subscribers_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func TestScannerReturnsErrorWhenClientMissing(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSecurityLake,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:securitylake:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
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
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
