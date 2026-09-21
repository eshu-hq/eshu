// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fis

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testTemplateARN = "arn:aws:fis:us-east-1:123456789012:experiment-template/EXTabc123"
	testRoleARN     = "arn:aws:iam::123456789012:role/fis-execution"
	testInstanceARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0"
	testClusterARN  = "arn:aws:ecs:us-east-1:123456789012:cluster/prod"
	testDBARN       = "arn:aws:rds:us-east-1:123456789012:db:orders-writer"
	testRDSClusARN  = "arn:aws:rds:us-east-1:123456789012:cluster:orders"
	testLogGroupARN = "arn:aws:logs:us-east-1:123456789012:log-group:/fis/experiments:*"
	testAlarmARN    = "arn:aws:cloudwatch:us-east-1:123456789012:alarm:fis-abort"
)

func fullTemplate() ExperimentTemplate {
	return ExperimentTemplate{
		ID:          "EXTabc123",
		ARN:         testTemplateARN,
		Name:        "stop-prod-instances",
		Description: "Inject EC2 stop fault",
		RoleARN:     testRoleARN,
		Actions: []Action{{
			Key:      "stop-instances",
			ActionID: "aws:ec2:stop-instances",
		}},
		Targets: []Target{{
			Key:           "instances",
			ResourceType:  "aws:ec2:instance",
			SelectionMode: "ALL",
			ResourceARNs:  []string{testInstanceARN},
		}, {
			Key:          "cluster",
			ResourceType: "aws:ecs:cluster",
			ResourceARNs: []string{testClusterARN},
		}, {
			Key:          "db",
			ResourceType: "aws:rds:db-instance",
			ResourceARNs: []string{testDBARN},
		}, {
			Key:          "dbcluster",
			ResourceType: "aws:rds:cluster",
			ResourceARNs: []string{testRDSClusARN},
		}},
		LogGroupARN:            testLogGroupARN,
		LogS3Bucket:            "fis-logs",
		LogS3Prefix:            "experiments/",
		StopConditionAlarmARNs: []string{testAlarmARN},
		CreationTime:           time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastUpdateTime:         time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:                   map[string]string{"Name": "stop-prod-instances", "Environment": "prod"},
	}
}

func TestScannerEmitsTemplateMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{fullTemplate()}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	template := resourceByType(t, envelopes, awscloud.ResourceTypeFISExperimentTemplate)
	if got, want := template.Payload["resource_id"], testTemplateARN; got != want {
		t.Fatalf("template resource_id = %#v, want %q", got, want)
	}
	if got, want := template.Payload["name"], "stop-prod-instances"; got != want {
		t.Fatalf("template name = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, template)
	assertAttribute(t, attrs, "template_id", "EXTabc123")
	assertAttribute(t, attrs, "action_ids", []string{"aws:ec2:stop-instances"})
	assertAttribute(t, attrs, "cloudwatch_logging", true)
	assertAttribute(t, attrs, "s3_logging", true)

	// template -> IAM role, keyed by the role ARN the IAM scanner publishes.
	role := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateUsesIAMRole)
	assertEdgeTarget(t, role, awscloud.ResourceTypeIAMRole, testRoleARN)

	// template -> EC2 instance, keyed by the bare i- id; target_arn stays empty
	// because the instance node is bare-id-keyed. The full ARN is an attribute.
	ec2 := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateTargetsEC2Instance)
	assertEdgeTarget(t, ec2, "aws_ec2_instance", "i-1234567890abcdef0")
	if got := ec2.Payload["target_arn"]; got != "" {
		t.Fatalf("ec2 target_arn = %#v, want empty (bare-id-keyed instance node)", got)
	}
	ec2Attrs := attributesOf(t, ec2)
	if got, want := ec2Attrs["instance_arn"], testInstanceARN; got != want {
		t.Fatalf("ec2 instance_arn attribute = %#v, want %q", got, want)
	}

	// template -> ECS cluster, keyed by the cluster ARN.
	ecs := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateTargetsECSCluster)
	assertEdgeTarget(t, ecs, awscloud.ResourceTypeECSCluster, testClusterARN)

	// template -> RDS DB instance and cluster, keyed by ARN.
	dbInstance := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateTargetsRDSDBInstance)
	assertEdgeTarget(t, dbInstance, awscloud.ResourceTypeRDSDBInstance, testDBARN)
	dbCluster := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateTargetsRDSDBCluster)
	assertEdgeTarget(t, dbCluster, awscloud.ResourceTypeRDSDBCluster, testRDSClusARN)

	// template -> CloudWatch log group, keyed by the bare log group ARN (no :*).
	logGroup := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateLogsToCloudWatchLogGroup)
	assertEdgeTarget(t, logGroup, awscloud.ResourceTypeCloudWatchLogsLogGroup,
		"arn:aws:logs:us-east-1:123456789012:log-group:/fis/experiments")

	// template -> S3 bucket, keyed by the synthesized partition-aware ARN.
	s3 := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateLogsToS3)
	assertEdgeTarget(t, s3, awscloud.ResourceTypeS3Bucket, "arn:aws:s3:::fis-logs")

	// template -> CloudWatch alarm stop condition, keyed by the alarm ARN.
	alarm := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateStopsOnCloudWatchAlarm)
	assertEdgeTarget(t, alarm, awscloud.ResourceTypeCloudWatchAlarm, testAlarmARN)

	// No action parameter values, target filter values, or run output leakage.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"action_parameters", "parameters", "target_filters", "filters",
			"resource_tags", "experiment_results", "resolved_targets",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; FIS scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	template := ExperimentTemplate{
		ID:          "EXTgov",
		ARN:         "arn:aws-us-gov:fis:us-gov-west-1:123456789012:experiment-template/EXTgov",
		LogS3Bucket: "gov-fis-logs",
	}
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{template}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3 := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateLogsToS3)
	wantARN := "arn:aws-us-gov:s3:::gov-fis-logs"
	if got := s3.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud template->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	template := ExperimentTemplate{
		ID:          "EXTcn",
		ARN:         "arn:aws-cn:fis:cn-north-1:123456789012:experiment-template/EXTcn",
		LogS3Bucket: "cn-fis-logs",
	}
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{template}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3 := relationshipByType(t, envelopes, awscloud.RelationshipFISTemplateLogsToS3)
	if got, want := s3.Payload["target_arn"], "arn:aws-cn:s3:::cn-fis-logs"; got != want {
		t.Fatalf("China template->s3 target_arn = %#v, want %q", got, want)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{{
		ID:  "EXTbare",
		ARN: "arn:aws:fis:us-east-1:123456789012:experiment-template/EXTbare",
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

func TestScannerSkipsUnmodeledTargetARNFamily(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{{
		ID:  "EXTeks",
		ARN: testTemplateARN,
		Targets: []Target{{
			Key:          "eks",
			ResourceType: "aws:eks:nodegroup",
			// EKS nodegroups are not modeled as an FIS target edge family.
			ResourceARNs: []string{"arn:aws:eks:us-east-1:123456789012:nodegroup/c/ng/abc"},
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship for unmodeled target family: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsIAMEdgeForNonARNRole(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{{
		ID:      "EXTrole",
		ARN:     testTemplateARN,
		RoleARN: "fis-execution",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship for non-ARN role: %#v", envelope.Payload)
		}
	}
}

func TestScannerDeduplicatesRepeatedTargetARN(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Templates: []ExperimentTemplate{{
		ID:  "EXTdup",
		ARN: testTemplateARN,
		Targets: []Target{{
			Key:          "a",
			ResourceType: "aws:ec2:instance",
			ResourceARNs: []string{testInstanceARN},
		}, {
			Key:          "b",
			ResourceType: "aws:ec2:instance",
			ResourceARNs: []string{testInstanceARN},
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("relationship count = %d, want 1 (duplicate target ARN must collapse)", count)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	observations := templateRelationships(testBoundary(), fullTemplate())
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Templates: []ExperimentTemplate{{ID: "EXTwarn", ARN: testTemplateARN}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "FIS GetExperimentTemplate throttled after SDK retries; template metadata omitted for this scan",
			SourceRecordID: "fis_templates_throttled",
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
		ServiceKind:         awscloud.ServiceFIS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:fis:1",
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
