// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package licensemanager

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testConfigARN   = "arn:aws:license-manager:us-east-1:123456789012:license-configuration:lic-0abc123"
	testConfigID    = "lic-0abc123"
	testInstanceARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	testInstanceID  = "i-0123456789abcdef0"
	testHostARN     = "arn:aws:ec2:us-east-1:123456789012:dedicated-host/h-0123456789abcdef0"
	testAMIARN      = "arn:aws:ec2:us-east-1:123456789012:image/ami-0123456789abcdef0"
)

func TestScannerEmitsConfigurationMetadataAndInstanceEdge(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:                     testConfigARN,
		ID:                      testConfigID,
		Name:                    "windows-server",
		Status:                  "AVAILABLE",
		LicenseCountingType:     "Instance",
		LicenseCount:            100,
		LicenseCountConfigured:  true,
		LicenseCountHardLimit:   true,
		ConsumedLicenses:        12,
		LicenseRuleCount:        2,
		ProductInformationCount: 1,
		OwnerAccountID:          "123456789012",
		LicenseExpiry:           time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Tags:                    map[string]string{"Environment": "prod"},
		Associations: []Association{
			{ResourceARN: testInstanceARN, ResourceType: "EC2_INSTANCE", ResourceOwnerID: "123456789012"},
			{ResourceARN: testHostARN, ResourceType: "EC2_HOST"},
			{ResourceARN: testAMIARN, ResourceType: "EC2_AMI"},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	config := resourceByType(t, envelopes, awscloud.ResourceTypeLicenseManagerConfiguration)
	if got, want := config.Payload["resource_id"], testConfigARN; got != want {
		t.Fatalf("config resource_id = %#v, want %q", got, want)
	}
	if got, want := config.Payload["state"], "AVAILABLE"; got != want {
		t.Fatalf("config state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, config)
	assertAttribute(t, attrs, "license_configuration_id", testConfigID)
	assertAttribute(t, attrs, "license_counting_type", "Instance")
	assertAttribute(t, attrs, "license_count", int64(100))
	assertAttribute(t, attrs, "license_count_hard_limit", true)
	assertAttribute(t, attrs, "consumed_licenses", int64(12))
	assertAttribute(t, attrs, "association_count", 3)
	assertAttribute(t, attrs, "associated_resource_types", []string{"EC2_AMI", "EC2_HOST", "EC2_INSTANCE"})

	// Only one relationship: configuration -> EC2 instance, keyed by bare i- id.
	edge := relationshipByType(t, envelopes, awscloud.RelationshipLicenseManagerConfigurationAppliesToInstance)
	assertEdgeTarget(t, edge, "aws_ec2_instance", testInstanceID)
	if got, want := edge.Payload["source_resource_id"], testConfigARN; got != want {
		t.Fatalf("edge source_resource_id = %#v, want %q", got, want)
	}
	if got, want := edge.Payload["source_arn"], testConfigARN; got != want {
		t.Fatalf("edge source_arn = %#v, want %q", got, want)
	}

	// EC2_HOST and EC2_AMI associations must NOT produce edges (no resolvable node).
	relationshipCount := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			relationshipCount++
		}
	}
	if relationshipCount != 1 {
		t.Fatalf("relationship count = %d, want 1 (host/AMI associations must not key edges)", relationshipCount)
	}
}

func TestScannerOmitsLicenseCountWhenUnset(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:                    testConfigARN,
		ID:                     testConfigID,
		Name:                   "no-count",
		LicenseCountConfigured: false,
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	config := resourceByType(t, envelopes, awscloud.ResourceTypeLicenseManagerConfiguration)
	attrs := attributesOf(t, config)
	if _, exists := attrs["license_count"]; exists {
		t.Fatalf("license_count attribute present, want omitted when not configured")
	}
}

func TestScannerSkipsEdgeForNonInstanceAssociations(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:  testConfigARN,
		ID:   testConfigID,
		Name: "rds-only",
		Associations: []Association{
			{ResourceARN: "arn:aws:rds:us-east-1:123456789012:db:orders", ResourceType: "RDS"},
			{ResourceARN: testHostARN, ResourceType: "EC2_HOST"},
		},
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

func TestScannerSkipsEdgeForMalformedInstanceARN(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:  testConfigARN,
		ID:   testConfigID,
		Name: "bad-arn",
		Associations: []Association{
			{ResourceARN: "not-an-arn", ResourceType: "EC2_INSTANCE"},
			{ResourceARN: "arn:aws:ec2:us-east-1:123456789012:volume/vol-123", ResourceType: "EC2_INSTANCE"},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted for malformed instance ARN: %#v", envelope.Payload)
		}
	}
}

func TestScannerResolvesGovCloudInstanceID(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govConfigARN := "arn:aws-us-gov:license-manager:us-gov-west-1:123456789012:license-configuration:lic-gov"
	govInstanceARN := "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:instance/i-govabc123"
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:          govConfigARN,
		ID:           "lic-gov",
		Name:         "gov-windows",
		Associations: []Association{{ResourceARN: govInstanceARN, ResourceType: "EC2_INSTANCE"}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipLicenseManagerConfigurationAppliesToInstance)
	if got, want := edge.Payload["target_resource_id"], "i-govabc123"; got != want {
		t.Fatalf("GovCloud edge target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	association := Association{ResourceARN: testInstanceARN, ResourceType: "EC2_INSTANCE"}
	rel := configurationInstanceRelationship(boundary, testConfigARN, testConfigARN, association)
	if rel == nil {
		t.Fatalf("expected non-nil relationship for EC2 instance association")
	}
	relguard.AssertObservations(t, *rel)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerErrorsWithoutClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Configurations: []Configuration{{ARN: testConfigARN, ID: testConfigID, Name: "windows"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "License Manager ListAssociationsForLicenseConfiguration throttled after SDK retries; associations omitted for this scan",
			SourceRecordID: "licensemanager_associations_throttled",
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

func TestScannerOmitsNoEntitlementLeakage(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Configurations: []Configuration{{
		ARN:                    testConfigARN,
		ID:                     testConfigID,
		Name:                   "windows",
		LicenseCount:           5,
		LicenseCountConfigured: true,
	}}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"entitlement", "entitlements", "access_token", "license_token",
			"checkout", "usage_records", "license_rules", "product_information",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; License Manager scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLicenseManager,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:licensemanager:1",
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
