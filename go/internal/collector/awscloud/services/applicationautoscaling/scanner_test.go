// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testRoleARN  = "arn:aws:iam::123456789012:role/aas-service-role"
	testPolicy   = "arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:uuid:resource/dynamodb/table/orders:policyName/orders-read"
	testAlarmARN = "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-read-high"
)

func TestScannerEmitsScalableTargetAndResourceEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ScalableTargets: []ScalableTarget{
			{
				ServiceNamespace:  "dynamodb",
				ResourceID:        "table/orders",
				ScalableDimension: "dynamodb:table:ReadCapacityUnits",
				RoleARN:           testRoleARN,
				MinCapacity:       aws.Int32(5),
				MaxCapacity:       aws.Int32(50),
				CreationTime:      time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			},
			{
				ServiceNamespace:  "ecs",
				ResourceID:        "service/prod/api",
				ScalableDimension: "ecs:service:DesiredCount",
			},
			{
				ServiceNamespace:  "rds",
				ResourceID:        "cluster:orders",
				ScalableDimension: "rds:cluster:ReadReplicaCount",
			},
			{
				ServiceNamespace:  "lambda",
				ResourceID:        "function:enricher:prod",
				ScalableDimension: "lambda:function:ProvisionedConcurrency",
			},
			{
				// Unmapped namespace: target node emitted, no scale edge.
				ServiceNamespace:  "sagemaker",
				ResourceID:        "endpoint/my-endpoint/variant/v1",
				ScalableDimension: "sagemaker:variant:DesiredInstanceCount",
			},
		},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	target := resourceByID(t, envelopes, "dynamodb/dynamodb:table:ReadCapacityUnits/table/orders")
	if got, want := target.Payload["resource_type"], awscloud.ResourceTypeApplicationAutoScalingScalableTarget; got != want {
		t.Fatalf("target resource_type = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, target)
	assertAttribute(t, attrs, "service_namespace", "dynamodb")
	assertAttribute(t, attrs, "role_arn", testRoleARN)
	assertAttribute(t, attrs, "min_capacity", int32(5))
	assertAttribute(t, attrs, "max_capacity", int32(50))

	// dynamodb edge -> partition-aware table ARN.
	ddb := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingTargetScalesDynamoDBTable)
	assertEdgeTarget(t, ddb, awscloud.ResourceTypeDynamoDBTable, "arn:aws:dynamodb:us-east-1:123456789012:table/orders")

	// ecs edge -> long-format service ARN.
	ecs := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingTargetScalesECSService)
	assertEdgeTarget(t, ecs, awscloud.ResourceTypeECSService, "arn:aws:ecs:us-east-1:123456789012:service/prod/api")

	// rds edge -> aurora cluster ARN.
	rds := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingTargetScalesRDSCluster)
	assertEdgeTarget(t, rds, awscloud.ResourceTypeRDSDBCluster, "arn:aws:rds:us-east-1:123456789012:cluster:orders")

	// lambda edge -> base function ARN (qualifier dropped).
	lambda := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingTargetScalesLambdaFunction)
	assertEdgeTarget(t, lambda, awscloud.ResourceTypeLambdaFunction, "arn:aws:lambda:us-east-1:123456789012:function:enricher")

	// sagemaker namespace emits a node but no scale edge.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == "endpoint/my-endpoint/variant/v1" {
			t.Fatalf("unmapped sagemaker namespace produced a dangling edge: %#v", envelope.Payload)
		}
	}
	_ = resourceByID(t, envelopes, "sagemaker/sagemaker:variant:DesiredInstanceCount/endpoint/my-endpoint/variant/v1")
}

func TestScannerEmitsPolicyAlarmAndScalableTargetEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ScalingPolicies: []ScalingPolicy{{
			ARN:               testPolicy,
			Name:              "orders-read",
			PolicyType:        "TargetTrackingScaling",
			ServiceNamespace:  "dynamodb",
			ResourceID:        "table/orders",
			ScalableDimension: "dynamodb:table:ReadCapacityUnits",
			AlarmARNs:         []string{testAlarmARN, "  "},
			CreationTime:      time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	policy := resourceByID(t, envelopes, testPolicy)
	if got, want := policy.Payload["resource_type"], awscloud.ResourceTypeApplicationAutoScalingScalingPolicy; got != want {
		t.Fatalf("policy resource_type = %#v, want %q", got, want)
	}

	// policy -> scalable target edge.
	pst := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingPolicyForScalableTarget)
	assertEdgeTarget(t, pst, awscloud.ResourceTypeApplicationAutoScalingScalableTarget,
		"dynamodb/dynamodb:table:ReadCapacityUnits/table/orders")
	if got, want := pst.Payload["source_resource_id"], testPolicy; got != want {
		t.Fatalf("policy->target source_resource_id = %#v, want %q", got, want)
	}

	// policy -> cloudwatch alarm edge keyed by alarm ARN.
	alarm := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm)
	assertEdgeTarget(t, alarm, awscloud.ResourceTypeCloudWatchAlarm, testAlarmARN)
	if got, want := alarm.Payload["target_arn"], testAlarmARN; got != want {
		t.Fatalf("policy->alarm target_arn = %#v, want %q", got, want)
	}
}

func TestScannerEmitsScheduledActionEdge(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ScheduledActions: []ScheduledAction{{
			ARN:               "arn:aws:autoscaling:us-east-1:123456789012:scheduledAction:uuid:resource/ecs/service/prod/api:scheduledActionName/scale-up",
			Name:              "scale-up",
			ServiceNamespace:  "ecs",
			ResourceID:        "service/prod/api",
			ScalableDimension: "ecs:service:DesiredCount",
			Schedule:          "cron(0 8 * * ? *)",
			Timezone:          "UTC",
			MinCapacity:       aws.Int32(2),
			MaxCapacity:       aws.Int32(10),
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	action := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingScheduledActionForScalableTarget)
	assertEdgeTarget(t, action, awscloud.ResourceTypeApplicationAutoScalingScalableTarget,
		"ecs/ecs:service:DesiredCount/service/prod/api")
}

func TestScannerSynthesizesGovCloudTargetARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{ScalableTargets: []ScalableTarget{{
		ServiceNamespace:  "dynamodb",
		ResourceID:        "table/gov-orders",
		ScalableDimension: "dynamodb:table:WriteCapacityUnits",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	ddb := relationshipByType(t, envelopes, awscloud.RelationshipApplicationAutoScalingTargetScalesDynamoDBTable)
	wantARN := "arn:aws-us-gov:dynamodb:us-gov-west-1:123456789012:table/gov-orders"
	if got := ddb.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud dynamodb target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerSkipsDynamoDBIndexTarget(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ScalableTargets: []ScalableTarget{{
		ServiceNamespace:  "dynamodb",
		ResourceID:        "table/orders/index/by-region",
		ScalableDimension: "dynamodb:index:ReadCapacityUnits",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("GSI target produced an edge that would mis-key the table node: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	var observations []awscloud.RelationshipObservation
	for _, target := range []ScalableTarget{
		{ServiceNamespace: "dynamodb", ResourceID: "table/orders", ScalableDimension: "dynamodb:table:ReadCapacityUnits"},
		{ServiceNamespace: "ecs", ResourceID: "service/prod/api", ScalableDimension: "ecs:service:DesiredCount"},
		{ServiceNamespace: "rds", ResourceID: "cluster:orders", ScalableDimension: "rds:cluster:ReadReplicaCount"},
		{ServiceNamespace: "lambda", ResourceID: "function:enricher:prod", ScalableDimension: "lambda:function:ProvisionedConcurrency"},
	} {
		rel := targetScalesResourceRelationship(boundary, target)
		if rel == nil {
			t.Fatalf("expected non-nil scale edge for %q", target.ServiceNamespace)
		}
		observations = append(observations, *rel)
	}
	policy := ScalingPolicy{
		ARN: testPolicy, Name: "orders-read", ServiceNamespace: "dynamodb",
		ResourceID: "table/orders", ScalableDimension: "dynamodb:table:ReadCapacityUnits",
		AlarmARNs: []string{testAlarmARN},
	}
	observations = append(observations, *policyForScalableTargetRelationship(boundary, policy))
	observations = append(observations, policyAlarmRelationships(boundary, policy)...)
	action := ScheduledAction{
		ARN: "arn:aws:autoscaling:us-east-1:123456789012:scheduledAction:x", Name: "scale-up",
		ServiceNamespace: "ecs", ResourceID: "service/prod/api", ScalableDimension: "ecs:service:DesiredCount",
	}
	observations = append(observations, *scheduledActionForScalableTargetRelationship(boundary, action))
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
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Application Auto Scaling DescribeScalingPolicies throttled after SDK retries; scaling_policies metadata omitted for namespace dynamodb",
			SourceRecordID: "applicationautoscaling_scaling_policies_throttled_dynamodb",
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

func TestScannerEmptyAccountReturnsNoFacts(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty account produced %d envelopes, want 0", len(envelopes))
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceApplicationAutoScaling,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:applicationautoscaling:1",
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

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_id %q", resourceID)
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
	t.Fatalf("missing relationship_type %q", relationshipType)
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
	t.Fatalf("missing warning_kind %q", warningKind)
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
