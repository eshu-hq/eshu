// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testCanaryARN = "arn:aws:synthetics:us-east-1:123456789012:canary:checkout-probe"
	testRoleARN   = "arn:aws:iam::123456789012:role/canary-exec"
)

func TestScannerEmitsCanaryMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Canaries: []Canary{{
		ARN:                          testCanaryARN,
		ID:                           "abcd1234-5678",
		Name:                         "checkout-probe",
		RuntimeVersion:               "syn-nodejs-puppeteer-7.0",
		State:                        "RUNNING",
		ScheduleExpression:           "rate(5 minutes)",
		ScheduleDurationInSeconds:    0,
		SuccessRetentionPeriodInDays: 31,
		FailureRetentionPeriodInDays: 31,
		RunTimeoutInSeconds:          60,
		RunMemoryInMB:                1024,
		RunActiveTracing:             true,
		ArtifactS3Location:           "checkout-artifacts/canary/checkout-probe",
		ArtifactEncryptionMode:       "SSE_KMS",
		ArtifactKMSKeyARN:            "arn:aws:kms:us-east-1:123456789012:key/abc",
		ExecutionRoleARN:             testRoleARN,
		VPCID:                        "vpc-0a1b2c3d",
		SubnetIDs:                    []string{"subnet-1111", "subnet-2222"},
		SecurityGroupIDs:             []string{"sg-9999"},
		Created:                      time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastModified:                 time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:                         map[string]string{"Environment": "prod"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	canary := resourceByType(t, envelopes, awscloud.ResourceTypeSyntheticsCanary)
	if got, want := canary.Payload["resource_id"], testCanaryARN; got != want {
		t.Fatalf("canary resource_id = %#v, want %q", got, want)
	}
	if got, want := canary.Payload["arn"], testCanaryARN; got != want {
		t.Fatalf("canary arn = %#v, want %q", got, want)
	}
	if got, want := canary.Payload["state"], "RUNNING"; got != want {
		t.Fatalf("canary state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, canary)
	assertAttribute(t, attrs, "runtime_version", "syn-nodejs-puppeteer-7.0")
	assertAttribute(t, attrs, "schedule_expression", "rate(5 minutes)")
	assertAttribute(t, attrs, "run_timeout_in_seconds", int32(60))
	assertAttribute(t, attrs, "run_memory_in_mb", int32(1024))
	assertAttribute(t, attrs, "run_active_tracing", true)
	assertAttribute(t, attrs, "vpc_id", "vpc-0a1b2c3d")
	assertAttribute(t, attrs, "vpc_configured", true)

	// canary -> S3 artifact bucket, keyed by the partition-aware bucket ARN the
	// S3 scanner publishes (extracted from the artifact location path).
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipSyntheticsCanaryUsesS3Bucket)
	wantBucketARN := "arn:aws:s3:::checkout-artifacts"
	assertEdgeTarget(t, s3Edge, awscloud.ResourceTypeS3Bucket, wantBucketARN)
	if got, want := s3Edge.Payload["source_resource_id"], testCanaryARN; got != want {
		t.Fatalf("canary->s3 source_resource_id = %#v, want %q", got, want)
	}

	// canary -> IAM execution role, keyed by the role ARN the IAM scanner publishes.
	roleEdge := relationshipByType(t, envelopes, awscloud.RelationshipSyntheticsCanaryUsesIAMRole)
	assertEdgeTarget(t, roleEdge, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := roleEdge.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("canary->role target_arn = %#v, want %q", got, want)
	}

	// canary -> subnet edges, keyed by bare subnet ids.
	subnetEdges := relationshipsByType(envelopes, awscloud.RelationshipSyntheticsCanaryUsesSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("subnet edges = %d, want 2", len(subnetEdges))
	}
	gotSubnets := map[string]bool{}
	for _, edge := range subnetEdges {
		if got := edge.Payload["target_type"]; got != awscloud.ResourceTypeEC2Subnet {
			t.Fatalf("subnet edge target_type = %#v, want %q", got, awscloud.ResourceTypeEC2Subnet)
		}
		gotSubnets[edge.Payload["target_resource_id"].(string)] = true
	}
	for _, want := range []string{"subnet-1111", "subnet-2222"} {
		if !gotSubnets[want] {
			t.Fatalf("missing subnet edge for %q in %#v", want, gotSubnets)
		}
	}

	// canary -> security group edge, keyed by bare sg id.
	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipSyntheticsCanaryUsesSecurityGroup)
	assertEdgeTarget(t, sgEdge, awscloud.ResourceTypeEC2SecurityGroup, "sg-9999")
	if got := sgEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("sg edge target_arn = %#v, want empty for bare sg id", got)
	}

	// No script source or run artifacts leak anywhere in resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"code", "handler", "script", "source_code", "zip_file", "zipfile",
			"source_location_arn", "runs", "run_results", "artifacts",
			"screenshots", "har", "logs",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Synthetics scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Canaries: []Canary{{
		ARN:                "arn:aws-us-gov:synthetics:us-gov-west-1:123456789012:canary:gov-probe",
		Name:               "gov-probe",
		ArtifactS3Location: "s3://gov-artifacts/canary",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipSyntheticsCanaryUsesS3Bucket)
	wantARN := "arn:aws-us-gov:s3:::gov-artifacts"
	if got := s3Edge.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud canary->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
	if got := s3Edge.Payload["target_arn"]; got != wantARN {
		t.Fatalf("GovCloud canary->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsEdgesWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Canaries: []Canary{{
		ARN:  testCanaryARN,
		Name: "checkout-probe",
		// No artifact location, role, or VPC config: no edges.
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
	canary := resourceByType(t, envelopes, awscloud.ResourceTypeSyntheticsCanary)
	if got := attributesOf(t, canary)["vpc_configured"]; got != false {
		t.Fatalf("vpc_configured = %#v, want false", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	canary := Canary{
		ARN:                testCanaryARN,
		Name:               "checkout-probe",
		ArtifactS3Location: "checkout-artifacts/canary",
		ExecutionRoleARN:   testRoleARN,
		SubnetIDs:          []string{"subnet-1111"},
		SecurityGroupIDs:   []string{"sg-9999"},
	}
	observations := canaryRelationships(boundary, canary)
	if len(observations) != 4 {
		t.Fatalf("relationships = %d, want 4 (s3, role, subnet, sg)", len(observations))
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Canaries: []Canary{{ARN: testCanaryARN, Name: "checkout-probe"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Synthetics DescribeCanaries throttled after SDK retries; canary metadata omitted for this scan",
			SourceRecordID: "synthetics_canaries_throttled",
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSynthetics,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:synthetics:1",
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
	edges := relationshipsByType(envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
	return edges[0]
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var edges []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			edges = append(edges, envelope)
		}
	}
	return edges
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
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
