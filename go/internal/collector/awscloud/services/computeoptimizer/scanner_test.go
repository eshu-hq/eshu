// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testInstanceARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890"
	testVolumeARN   = "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc123def4567890"
	testFunctionARN = "arn:aws:lambda:us-east-1:123456789012:function:checkout"
	testASGARN      = "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web-asg"
)

func fullSnapshot() Snapshot {
	refresh := time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC)
	return Snapshot{
		Summaries: []RecommendationSummary{{
			ResourceType:                 "Ec2Instance",
			AccountID:                    "123456789012",
			FindingCounts:                map[string]float64{"Optimized": 3, "Overprovisioned": 1},
			SavingsOpportunityPercentage: 12.5,
		}},
		InstanceRecommendations: []InstanceRecommendation{{
			InstanceARN:                  testInstanceARN,
			InstanceName:                 "checkout-1",
			AccountID:                    "123456789012",
			CurrentInstanceType:          "m5.2xlarge",
			Finding:                      "Overprovisioned",
			RecommendedInstanceType:      "m5.xlarge",
			LookBackPeriodInDays:         14,
			SavingsOpportunityPercentage: 30,
			LastRefreshTimestamp:         refresh,
			Tags:                         map[string]string{"Environment": "prod"},
		}},
		AutoScalingGroupRecommendations: []AutoScalingGroupRecommendation{{
			AutoScalingGroupARN:          testASGARN,
			AutoScalingGroupName:         "web-asg",
			AccountID:                    "123456789012",
			CurrentInstanceType:          "c5.large",
			Finding:                      "NotOptimized",
			RecommendedInstanceType:      "c6g.large",
			LookBackPeriodInDays:         14,
			SavingsOpportunityPercentage: 18,
			LastRefreshTimestamp:         refresh,
		}},
		VolumeRecommendations: []VolumeRecommendation{{
			VolumeARN:             testVolumeARN,
			AccountID:             "123456789012",
			CurrentVolumeType:     "gp2",
			Finding:               "NotOptimized",
			RecommendedVolumeType: "gp3",
			LookBackPeriodInDays:  14,
			LastRefreshTimestamp:  refresh,
			Tags:                  map[string]string{"Team": "storage"},
		}},
		LambdaFunctionRecommendations: []LambdaFunctionRecommendation{{
			FunctionARN:                  testFunctionARN,
			FunctionVersion:              "$LATEST",
			AccountID:                    "123456789012",
			CurrentMemorySize:            512,
			Finding:                      "NotOptimized",
			RecommendedMemorySize:        256,
			LookBackPeriodInDays:         14,
			SavingsOpportunityPercentage: 22,
			LastRefreshTimestamp:         refresh,
		}},
	}
}

func TestScannerEmitsRecommendationMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Summary resource.
	summaries := resourcesByType(t, envelopes, awscloud.ResourceTypeComputeOptimizerRecommendationSummary)
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}
	summaryAttrs := attributesOf(t, summaries[0])
	assertAttribute(t, summaryAttrs, "recommendation_resource_type", "Ec2Instance")
	assertAttribute(t, summaryAttrs, "savings_opportunity_percentage", 12.5)

	// Four recommendation resources (instance, asg, volume, lambda).
	recs := resourcesByType(t, envelopes, awscloud.ResourceTypeComputeOptimizerRecommendation)
	if len(recs) != 4 {
		t.Fatalf("recommendation count = %d, want 4", len(recs))
	}

	instance := recommendationByResourceID(t, envelopes, testInstanceARN)
	instAttrs := attributesOf(t, instance)
	assertAttribute(t, instAttrs, "current_instance_type", "m5.2xlarge")
	assertAttribute(t, instAttrs, "recommended_instance_type", "m5.xlarge")
	assertAttribute(t, instAttrs, "instance_id", "i-0abc123def4567890")

	// instance recommendation -> EC2 instance edge keyed by bare instance id.
	instEdge := relationshipByType(t, envelopes, awscloud.RelationshipComputeOptimizerRecommendationTargetsInstance)
	assertEdgeTarget(t, instEdge, "aws_ec2_instance", "i-0abc123def4567890")
	if got, want := instEdge.Payload["source_resource_id"], testInstanceARN; got != want {
		t.Fatalf("instance edge source_resource_id = %#v, want %q", got, want)
	}
	if got := instEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("instance edge target_arn = %#v, want empty (bare-id keyed)", got)
	}

	// asg recommendation -> ASG edge keyed by group name (no target_arn, since the
	// autoscaling scanner publishes its group resource_id as the bare name).
	asgEdge := relationshipByType(t, envelopes, awscloud.RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup)
	assertEdgeTarget(t, asgEdge, awscloud.ResourceTypeAutoScalingGroup, "web-asg")
	if got := asgEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("asg edge target_arn = %#v, want empty (name-keyed target)", got)
	}

	// lambda recommendation -> function edge keyed by ARN.
	lambdaEdge := relationshipByType(t, envelopes, awscloud.RelationshipComputeOptimizerRecommendationTargetsFunction)
	assertEdgeTarget(t, lambdaEdge, awscloud.ResourceTypeLambdaFunction, testFunctionARN)
	if got, want := lambdaEdge.Payload["target_arn"], testFunctionARN; got != want {
		t.Fatalf("lambda edge target_arn = %#v, want %q", got, want)
	}

	// EBS volume recommendation carries no edge in this scanner yet.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["target_type"].(string); got == awscloud.ResourceTypeEC2Volume {
			t.Fatalf("unexpected EBS volume edge emitted: %#v", envelope.Payload)
		}
	}

	// No utilization metric data points / cost values leak into any resource.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"utilization_metrics", "projected_utilization_metrics", "metric_values",
			"data_points", "estimated_monthly_savings", "currency",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Compute Optimizer scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerCountsExactlyThreeEdges(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edges := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			edges++
		}
	}
	if edges != 3 {
		t.Fatalf("relationship count = %d, want 3 (instance, asg, lambda; EBS skipped)", edges)
	}
}

func TestScannerHandlesOptInNotEnabled(t *testing.T) {
	// An account not enrolled in Compute Optimizer yields an empty snapshot, not
	// an error. The scan must return cleanly with no facts.
	envelopes, err := (Scanner{Client: fakeClient{snapshot: Snapshot{}}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil for not-enrolled account", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes, want 0 for empty snapshot", len(envelopes))
	}
}

func TestScannerOmitsInstanceEdgeForNonInstanceARN(t *testing.T) {
	snap := Snapshot{InstanceRecommendations: []InstanceRecommendation{{
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:not-an-instance/x",
		AccountID:   "123456789012",
		Finding:     "Optimized",
	}}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snap}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected edge for non-instance ARN: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	snap := fullSnapshot()
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		instanceTargetRelationship(boundary, snap.InstanceRecommendations[0]),
		autoScalingGroupTargetRelationship(boundary, snap.AutoScalingGroupRecommendations[0]),
		lambdaFunctionTargetRelationship(boundary, snap.LambdaFunctionRecommendations[0]),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerSupportsGovCloudInstanceEdge(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	snap := Snapshot{InstanceRecommendations: []InstanceRecommendation{{
		InstanceARN: "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:instance/i-0gov11112222",
		AccountID:   "123456789012",
		Finding:     "Underprovisioned",
	}}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snap}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipComputeOptimizerRecommendationTargetsInstance)
	if got, want := edge.Payload["target_resource_id"], "i-0gov11112222"; got != want {
		t.Fatalf("GovCloud instance edge target_resource_id = %#v, want %q", got, want)
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Compute Optimizer GetEC2InstanceRecommendations throttled after SDK retries; instance recommendations omitted for this scan",
			SourceRecordID: "compute_optimizer_instances_throttled",
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
		ServiceKind:         awscloud.ServiceComputeOptimizer,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:computeoptimizer:1",
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

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			out = append(out, envelope)
		}
	}
	return out
}

func recommendationByResourceID(t *testing.T, envelopes []facts.Envelope, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing recommendation with resource_id %q", resourceID)
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
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
