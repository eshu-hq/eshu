// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAppARN      = "arn:aws:resiliencehub:us-east-1:123456789012:app/app-1234"
	testPolicyARN   = "arn:aws:resiliencehub:us-east-1:123456789012:resiliency-policy/policy-5678"
	testAssessARN   = "arn:aws:resiliencehub:us-east-1:123456789012:app-assessment/app-1234/assess-1"
	testLambdaARN   = "arn:aws:lambda:us-east-1:123456789012:function:checkout"
	testTopicARN    = "arn:aws:sns:us-east-1:123456789012:checkout-events"
	testNativeEC2ID = "i-0abc123def456"
)

func TestScannerEmitsResilienceHubMetadataAndRelationships(t *testing.T) {
	rto := int32(7200)
	rpo := int32(3600)
	client := fakeClient{snapshot: Snapshot{
		Policies: []ResiliencyPolicy{{
			ARN:          testPolicyARN,
			Name:         "mission-critical",
			Tier:         "MissionCritical",
			CreationTime: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			FailureTargets: map[string]FailureTarget{
				"AZ": {RPOInSecs: 3600, RTOInSecs: 7200},
			},
			Tags: map[string]string{"Team": "platform"},
		}},
		Apps: []App{{
			ARN:                testAppARN,
			Name:               "checkout",
			Status:             "Active",
			ComplianceStatus:   "PolicyMet",
			DriftStatus:        "NotDetected",
			AssessmentSchedule: "Daily",
			PolicyARN:          testPolicyARN,
			ResiliencyScore:    0.95,
			RPOInSecs:          &rpo,
			RTOInSecs:          &rto,
			CreationTime:       time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
			Tags:               map[string]string{"Environment": "prod"},
			InputSources: []InputSource{{
				ImportType:    "CfnStack",
				SourceName:    "checkout-stack",
				SourceARN:     "arn:aws:cloudformation:us-east-1:123456789012:stack/checkout-stack/abc",
				ResourceCount: 12,
			}},
			Components: []AppComponent{{
				Name: "compute",
				Type: "AWS::ResilienceHub::ComputeAppComponent",
			}},
			ProtectedResources: []ProtectedResource{
				{ARN: testLambdaARN, ResilienceHubType: "AWS::Lambda::Function", LogicalResourceID: "CheckoutFn"},
				{ARN: testTopicARN, ResilienceHubType: "AWS::SNS::Topic"},
			},
			Assessments: []Assessment{{
				ARN:              testAssessARN,
				AppARN:           testAppARN,
				Name:             "weekly",
				Status:           "Success",
				ComplianceStatus: "PolicyMet",
				DriftStatus:      "NotDetected",
				Invoker:          "User",
				AppVersion:       "release",
				ResiliencyScore:  0.95,
				StartTime:        time.Date(2026, 5, 3, 1, 0, 0, 0, time.UTC),
				EndTime:          time.Date(2026, 5, 3, 1, 5, 0, 0, time.UTC),
			}},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Policy resource node.
	policy := resourceByType(t, envelopes, awscloud.ResourceTypeResilienceHubResiliencyPolicy)
	if got, want := policy.Payload["resource_id"], testPolicyARN; got != want {
		t.Fatalf("policy resource_id = %#v, want %q", got, want)
	}
	policyAttrs := attributesOf(t, policy)
	assertAttribute(t, policyAttrs, "tier", "MissionCritical")

	// App resource node.
	app := resourceByType(t, envelopes, awscloud.ResourceTypeResilienceHubApp)
	if got, want := app.Payload["resource_id"], testAppARN; got != want {
		t.Fatalf("app resource_id = %#v, want %q", got, want)
	}
	if got, want := app.Payload["state"], "Active"; got != want {
		t.Fatalf("app state = %#v, want %q", got, want)
	}
	appAttrs := attributesOf(t, app)
	assertAttribute(t, appAttrs, "compliance_status", "PolicyMet")
	assertAttribute(t, appAttrs, "rpo_in_secs", int32(3600))
	assertAttribute(t, appAttrs, "rto_in_secs", int32(7200))

	// Input source, component, assessment resource nodes.
	resourceByType(t, envelopes, awscloud.ResourceTypeResilienceHubAppInputSource)
	resourceByType(t, envelopes, awscloud.ResourceTypeResilienceHubAppComponent)
	assessment := resourceByType(t, envelopes, awscloud.ResourceTypeResilienceHubAppAssessment)
	if got, want := assessment.Payload["resource_id"], testAssessARN; got != want {
		t.Fatalf("assessment resource_id = %#v, want %q", got, want)
	}

	// app -> policy edge.
	appPolicy := relationshipByType(t, envelopes, awscloud.RelationshipResilienceHubAppUsesPolicy)
	assertEdgeTarget(t, appPolicy, awscloud.ResourceTypeResilienceHubResiliencyPolicy, testPolicyARN)
	if got, want := appPolicy.Payload["source_resource_id"], testAppARN; got != want {
		t.Fatalf("app->policy source_resource_id = %#v, want %q", got, want)
	}

	// app -> protected resource edges (Lambda + SNS, both ARN-keyed).
	protects := relationshipsByType(envelopes, awscloud.RelationshipResilienceHubAppProtectsResource)
	if len(protects) != 2 {
		t.Fatalf("expected 2 protects-resource edges, got %d", len(protects))
	}
	gotTargets := map[string]string{}
	for _, edge := range protects {
		gotTargets[edge.Payload["target_resource_id"].(string)] = edge.Payload["target_type"].(string)
	}
	if gotTargets[testLambdaARN] != awscloud.ResourceTypeLambdaFunction {
		t.Fatalf("lambda protect edge target_type = %q, want %q", gotTargets[testLambdaARN], awscloud.ResourceTypeLambdaFunction)
	}
	if gotTargets[testTopicARN] != awscloud.ResourceTypeSNSTopic {
		t.Fatalf("sns protect edge target_type = %q, want %q", gotTargets[testTopicARN], awscloud.ResourceTypeSNSTopic)
	}

	// component -> app, input source -> app, assessment -> app edges.
	componentEdge := relationshipByType(t, envelopes, awscloud.RelationshipResilienceHubComponentInApp)
	assertEdgeTarget(t, componentEdge, awscloud.ResourceTypeResilienceHubApp, testAppARN)
	inputEdge := relationshipByType(t, envelopes, awscloud.RelationshipResilienceHubInputSourceInApp)
	assertEdgeTarget(t, inputEdge, awscloud.ResourceTypeResilienceHubApp, testAppARN)
	assessEdge := relationshipByType(t, envelopes, awscloud.RelationshipResilienceHubAssessmentForApp)
	assertEdgeTarget(t, assessEdge, awscloud.ResourceTypeResilienceHubApp, testAppARN)

	// No assessment-result / drift / recommendation leakage anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"recommendations", "alarm_recommendations", "test_recommendations",
			"sop_recommendations", "compliance_drifts", "resource_drifts",
			"assessment_results", "policy_body",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Resilience Hub scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSkipsNativeProtectedResourceEdge(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Apps: []App{{
		ARN:  testAppARN,
		Name: "checkout",
		ProtectedResources: []ProtectedResource{
			// An EC2 instance carries an ARN-shaped value here only if mapped as
			// Arn; but native EC2 IDs never reach the scanner model. Even a
			// non-ARN identifier (defensive) must not produce an edge.
			{ARN: testNativeEC2ID, ResilienceHubType: "AWS::EC2::Instance"},
			// A type we deliberately do not key (DynamoDB is Native in RH) is
			// dropped even with an ARN-shaped identifier, because the mapping
			// returns no target type.
			{ARN: "arn:aws:dynamodb:us-east-1:123456789012:table/orders", ResilienceHubType: "AWS::DynamoDB::Table"},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if edges := relationshipsByType(envelopes, awscloud.RelationshipResilienceHubAppProtectsResource); len(edges) != 0 {
		t.Fatalf("expected no protects-resource edge for native/unkeyed resources, got %d", len(edges))
	}
}

func TestScannerOmitsPolicyEdgeWhenNoPolicyAttached(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Apps: []App{{
		ARN:  testAppARN,
		Name: "checkout",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got := envelope.Payload["relationship_type"]; got == awscloud.RelationshipResilienceHubAppUsesPolicy {
			t.Fatalf("unexpected app->policy edge for app with no policy: %#v", envelope.Payload)
		}
	}
}

func TestScannerPreservesGovCloudPartitionOnEdges(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govAppARN := "arn:aws-us-gov:resiliencehub:us-gov-west-1:123456789012:app/app-1"
	govLambdaARN := "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:checkout"
	client := fakeClient{snapshot: Snapshot{Apps: []App{{
		ARN:  govAppARN,
		Name: "checkout",
		ProtectedResources: []ProtectedResource{
			{ARN: govLambdaARN, ResilienceHubType: "AWS::Lambda::Function"},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipResilienceHubAppProtectsResource)
	if got := edge.Payload["target_resource_id"]; got != govLambdaARN {
		t.Fatalf("gov protect edge target_resource_id = %#v, want %q (partition must be preserved)", got, govLambdaARN)
	}
	if got := edge.Payload["target_arn"]; got != govLambdaARN {
		t.Fatalf("gov protect edge target_arn = %#v, want %q", got, govLambdaARN)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	app := App{
		ARN:       testAppARN,
		Name:      "checkout",
		PolicyARN: testPolicyARN,
		InputSources: []InputSource{{
			ImportType: "Resource",
			SourceName: "orders-group",
			SourceARN:  "arn:aws:resource-groups:us-east-1:123456789012:group/orders",
		}},
		Components: []AppComponent{{Name: "compute", Type: "AWS::ResilienceHub::ComputeAppComponent"}},
		ProtectedResources: []ProtectedResource{
			{ARN: testLambdaARN, ResilienceHubType: "AWS::Lambda::Function"},
		},
	}
	assessment := Assessment{ARN: testAssessARN, AppARN: testAppARN, Name: "weekly"}

	var observations []awscloud.RelationshipObservation
	candidates := []*awscloud.RelationshipObservation{
		appUsesPolicyRelationship(boundary, app),
		appProtectsResourceRelationship(boundary, app, app.ProtectedResources[0]),
		componentInAppRelationship(boundary, app, app.Components[0]),
		inputSourceInAppRelationship(boundary, app, app.InputSources[0]),
		assessmentForAppRelationship(boundary, assessment),
	}
	for _, rel := range candidates {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
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

func TestScannerEmitsVersionMissingWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Apps: []App{{ARN: testAppARN, Name: "checkout"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningResilienceHubAppVersionMissing,
			ErrorClass:     "resource_not_found",
			Message:        "Resilience Hub application has no release version",
			SourceRecordID: "resiliencehub_app_version_missing:" + testAppARN,
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningResilienceHubAppVersionMissing)
	if got := warning.Payload["error_class"]; got != "resource_not_found" {
		t.Fatalf("warning error_class = %#v, want resource_not_found", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() with nil client error = nil, want error")
	}
}
