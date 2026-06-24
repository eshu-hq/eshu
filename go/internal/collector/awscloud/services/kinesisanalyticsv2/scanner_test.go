// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesisanalyticsv2

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAppARN       = "arn:aws:kinesisanalytics:us-east-1:123456789012:application/orders-flink"
	testInputKDS     = "arn:aws:kinesis:us-east-1:123456789012:stream/orders-in"
	testOutputKDS    = "arn:aws:kinesis:us-east-1:123456789012:stream/orders-out"
	testInputFH      = "arn:aws:firehose:us-east-1:123456789012:deliverystream/orders-fh-in"
	testOutputFH     = "arn:aws:firehose:us-east-1:123456789012:deliverystream/orders-fh-out"
	testCodeBucket   = "arn:aws:s3:::orders-flink-code"
	testRoleARN      = "arn:aws:iam::123456789012:role/orders-flink-role"
	testLogGroupARN  = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/kinesis-analytics/orders-flink"
	testLogStreamARN = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/kinesis-analytics/orders-flink:log-stream:"
)

func fullApplication() Application {
	return Application{
		Name:                         "orders-flink",
		ARN:                          testAppARN,
		Status:                       "RUNNING",
		RuntimeEnvironment:           "FLINK-1_18",
		Mode:                         "STREAMING",
		Description:                  "orders streaming app",
		VersionID:                    7,
		VersionCount:                 7,
		ServiceExecutionRoleARN:      testRoleARN,
		CreateTimestamp:              time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastUpdateTimestamp:          time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		AutoScalingEnabled:           true,
		ParallelismConfigurationType: "CUSTOM",
		Parallelism:                  4,
		ParallelismPerKPU:            2,
		CurrentParallelism:           4,
		SnapshotsEnabled:             true,
		CodeContentType:              "ZIPFILE",
		CodeS3BucketARN:              testCodeBucket,
		CodeS3FileKey:                "code/orders-flink.zip",
		InputKinesisStreamARNs:       []string{testInputKDS},
		OutputKinesisStreamARNs:      []string{testOutputKDS},
		InputFirehoseStreamARNs:      []string{testInputFH},
		OutputFirehoseStreamARNs:     []string{testOutputFH},
		VPCConfigurations: []VPCConfiguration{{
			VPCConfigurationID: "1.1",
			VPCID:              "vpc-0a1b2c3d",
			SubnetIDs:          []string{"subnet-0a1b2c3d", "subnet-0e5f6a7b"},
			SecurityGroupIDs:   []string{"sg-0a1b2c3d"},
		}},
		LogGroupARNs: []string{testLogGroupARN},
		Snapshots: []Snapshot{{
			Name:                 "snapshot-001",
			Status:               "READY",
			ApplicationVersionID: 6,
		}},
		Tags: map[string]string{"Environment": "prod"},
	}
}

func TestScannerEmitsApplicationMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{applications: []Application{fullApplication()}}}).
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	app := resourceByType(t, envelopes, awscloud.ResourceTypeManagedFlinkApplication)
	if got, want := app.Payload["resource_id"], testAppARN; got != want {
		t.Fatalf("application resource_id = %#v, want %q", got, want)
	}
	if got, want := app.Payload["state"], "RUNNING"; got != want {
		t.Fatalf("application state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, app)
	assertAttribute(t, attrs, "runtime_environment", "FLINK-1_18")
	assertAttribute(t, attrs, "application_mode", "STREAMING")
	assertAttribute(t, attrs, "version_id", int64(7))
	assertAttribute(t, attrs, "auto_scaling_enabled", true)
	assertAttribute(t, attrs, "snapshots_enabled", true)
	assertAttribute(t, attrs, "parallelism", int32(4))
	assertAttribute(t, attrs, "snapshot_names", []string{"snapshot-001"})

	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationReadsFromKinesisStream,
		awscloud.ResourceTypeKinesisDataStream, testInputKDS, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationWritesToKinesisStream,
		awscloud.ResourceTypeKinesisDataStream, testOutputKDS, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationReadsFromFirehoseStream,
		awscloud.ResourceTypeFirehoseDeliveryStream, testInputFH, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationWritesToFirehoseStream,
		awscloud.ResourceTypeFirehoseDeliveryStream, testOutputFH, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationUsesS3CodeBucket,
		awscloud.ResourceTypeS3Bucket, testCodeBucket, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationUsesIAMRole,
		awscloud.ResourceTypeIAMRole, testRoleARN, testAppARN)
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationLogsToCloudWatchLogGroup,
		awscloud.ResourceTypeCloudWatchLogsLogGroup, testLogGroupARN, testAppARN)

	// Two distinct subnet edges, both bare ids.
	subnetTargets := relationshipTargets(envelopes, awscloud.RelationshipManagedFlinkApplicationUsesSubnet)
	if len(subnetTargets) != 2 {
		t.Fatalf("subnet edge count = %d, want 2 (%#v)", len(subnetTargets), subnetTargets)
	}
	assertEdge(t, envelopes, awscloud.RelationshipManagedFlinkApplicationUsesSecurityGroup,
		awscloud.ResourceTypeEC2SecurityGroup, "sg-0a1b2c3d", testAppARN)

	// Code body / SQL / environment property leakage must not appear.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		a, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"code", "text_content", "code_content", "sql", "sql_text",
			"environment_properties", "property_groups", "job_plan",
			"run_configuration", "records",
		} {
			if _, exists := a[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Managed Flink scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerDedupesAndKeysBareSubnetTargets(t *testing.T) {
	app := fullApplication()
	app.VPCConfigurations = []VPCConfiguration{
		{VPCID: "vpc-1", SubnetIDs: []string{"subnet-1"}, SecurityGroupIDs: []string{"sg-1"}},
		{VPCID: "vpc-1", SubnetIDs: []string{"subnet-1"}, SecurityGroupIDs: []string{"sg-1"}},
	}
	envelopes, err := (Scanner{Client: fakeClient{applications: []Application{app}}}).
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipTargets(envelopes, awscloud.RelationshipManagedFlinkApplicationUsesSubnet); len(got) != 1 {
		t.Fatalf("subnet edge count = %d, want 1 deduped (%#v)", len(got), got)
	}
	if got := relationshipTargets(envelopes, awscloud.RelationshipManagedFlinkApplicationUsesSecurityGroup); len(got) != 1 {
		t.Fatalf("security group edge count = %d, want 1 deduped (%#v)", len(got), got)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	app := Application{Name: "bare", ARN: "arn:aws:kinesisanalytics:us-east-1:123456789012:application/bare", Status: "READY"}
	envelopes, err := (Scanner{Client: fakeClient{applications: []Application{app}}}).
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerReturnsNilForEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if envelopes != nil {
		t.Fatalf("Scan() = %#v, want nil for empty account", envelopes)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	observations := applicationRelationships(boundary, fullApplication())
	if len(observations) == 0 {
		t.Fatalf("expected relationships for fully populated fixture")
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
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceKinesisAnalyticsV2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:kinesisanalyticsv2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	applications []Application
}

func (c fakeClient) ListApplications(context.Context) ([]Application, error) {
	return c.applications, nil
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

func relationshipByTarget(envelopes []facts.Envelope, relationshipType, targetResourceID string) (facts.Envelope, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetResourceID {
			return envelope, true
		}
	}
	return facts.Envelope{}, false
}

func relationshipTargets(envelopes []facts.Envelope, relationshipType string) []string {
	var targets []string
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		target, _ := envelope.Payload["target_resource_id"].(string)
		targets = append(targets, target)
	}
	return targets
}

func assertEdge(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType, targetType, targetResourceID, sourceResourceID string,
) {
	t.Helper()
	envelope, ok := relationshipByTarget(envelopes, relationshipType, targetResourceID)
	if !ok {
		t.Fatalf("missing %s edge to %q", relationshipType, targetResourceID)
	}
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("%s target_type = %#v, want %q", relationshipType, got, targetType)
	}
	if got := envelope.Payload["source_resource_id"]; got != sourceResourceID {
		t.Fatalf("%s source_resource_id = %#v, want %q", relationshipType, got, sourceResourceID)
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
