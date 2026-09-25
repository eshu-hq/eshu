// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsresiliencehubtypes "github.com/aws/aws-sdk-go-v2/service/resiliencehub/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	resiliencehubservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/resiliencehub"
)

const (
	stubAppARN    = "arn:aws:resiliencehub:us-east-1:123456789012:app/app-1"
	stubPolicyARN = "arn:aws:resiliencehub:us-east-1:123456789012:resiliency-policy/policy-1"
	stubLambdaARN = "arn:aws:lambda:us-east-1:123456789012:function:checkout"
)

func TestClientSnapshotMapsAppsPoliciesAndArnOnlyResources(t *testing.T) {
	stub := &stubAPI{
		policies: []awsresiliencehubtypes.ResiliencyPolicy{{
			PolicyArn:  awsv2.String(stubPolicyARN),
			PolicyName: awsv2.String("mission-critical"),
			Tier:       awsresiliencehubtypes.ResiliencyPolicyTierMissionCritical,
			Policy: map[string]awsresiliencehubtypes.FailurePolicy{
				"AZ": {RpoInSecs: 3600, RtoInSecs: 7200},
			},
			Tags: map[string]string{"Team": "platform"},
		}},
		apps: []awsresiliencehubtypes.AppSummary{{
			AppArn: awsv2.String(stubAppARN),
			Name:   awsv2.String("checkout"),
			Status: awsresiliencehubtypes.AppStatusTypeActive,
		}},
		appPolicyARN: stubPolicyARN,
		appTags:      map[string]string{"Environment": "prod"},
		inputSources: []awsresiliencehubtypes.AppInputSource{{
			ImportType: awsresiliencehubtypes.ResourceMappingTypeCfnStack,
			SourceName: awsv2.String("checkout-stack"),
			SourceArn:  awsv2.String("arn:aws:cloudformation:us-east-1:123456789012:stack/checkout/abc"),
		}},
		components: []awsresiliencehubtypes.AppComponent{{
			Name: awsv2.String("compute"),
			Type: awsv2.String("AWS::ResilienceHub::ComputeAppComponent"),
		}},
		resources: []awsresiliencehubtypes.PhysicalResource{
			{
				ResourceType:       awsv2.String("AWS::Lambda::Function"),
				LogicalResourceId:  &awsresiliencehubtypes.LogicalResourceId{Identifier: awsv2.String("CheckoutFn")},
				PhysicalResourceId: &awsresiliencehubtypes.PhysicalResourceId{Identifier: awsv2.String(stubLambdaARN), Type: awsresiliencehubtypes.PhysicalIdentifierTypeArn},
			},
			{
				// Native identifier must be dropped by the adapter.
				ResourceType:       awsv2.String("AWS::EC2::Instance"),
				LogicalResourceId:  &awsresiliencehubtypes.LogicalResourceId{Identifier: awsv2.String("WebServer")},
				PhysicalResourceId: &awsresiliencehubtypes.PhysicalResourceId{Identifier: awsv2.String("i-0abc"), Type: awsresiliencehubtypes.PhysicalIdentifierTypeNative},
			},
		},
		assessments: []awsresiliencehubtypes.AppAssessmentSummary{{
			AssessmentArn:    awsv2.String("arn:aws:resiliencehub:us-east-1:123456789012:app-assessment/app-1/a1"),
			AppArn:           awsv2.String(stubAppARN),
			AssessmentName:   awsv2.String("weekly"),
			AssessmentStatus: awsresiliencehubtypes.AssessmentStatusSuccess,
		}},
	}
	client := newTestClient(stub)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Policies) != 1 {
		t.Fatalf("policies = %d, want 1", len(snapshot.Policies))
	}
	if got := snapshot.Policies[0].FailureTargets["AZ"].RPOInSecs; got != 3600 {
		t.Fatalf("policy AZ RPO = %d, want 3600", got)
	}
	if len(snapshot.Apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(snapshot.Apps))
	}
	app := snapshot.Apps[0]
	if app.PolicyARN != stubPolicyARN {
		t.Fatalf("app PolicyARN = %q, want %q", app.PolicyARN, stubPolicyARN)
	}
	if app.Tags["Environment"] != "prod" {
		t.Fatalf("app tags = %#v, want Environment=prod", app.Tags)
	}
	if len(app.InputSources) != 1 || len(app.Components) != 1 || len(app.Assessments) != 1 {
		t.Fatalf("app metadata counts = sources %d components %d assessments %d, want 1/1/1",
			len(app.InputSources), len(app.Components), len(app.Assessments))
	}
	if len(app.ProtectedResources) != 1 {
		t.Fatalf("protected resources = %d, want 1 (native EC2 must be dropped)", len(app.ProtectedResources))
	}
	if app.ProtectedResources[0].ARN != stubLambdaARN {
		t.Fatalf("protected resource ARN = %q, want %q", app.ProtectedResources[0].ARN, stubLambdaARN)
	}
}

func TestClientSnapshotWarnsWhenPublishedVersionMissing(t *testing.T) {
	stub := &stubAPI{
		apps: []awsresiliencehubtypes.AppSummary{{
			AppArn: awsv2.String(stubAppARN),
			Name:   awsv2.String("never-published"),
		}},
		versionNotFound: true,
	}
	client := newTestClient(stub)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (missing version is a warning)", err)
	}
	if len(snapshot.Apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(snapshot.Apps))
	}
	if len(snapshot.Apps[0].ProtectedResources) != 0 {
		t.Fatalf("expected no protected resources when version missing")
	}
	if len(snapshot.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(snapshot.Warnings))
	}
	if snapshot.Warnings[0].WarningKind != aws.WarningResilienceHubAppVersionMissing {
		t.Fatalf("warning kind = %q, want %q", snapshot.Warnings[0].WarningKind, aws.WarningResilienceHubAppVersionMissing)
	}
}

func newTestClient(stub apiClient) *Client {
	return &Client{
		client: stub,
		boundary: aws.Boundary{
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         aws.ServiceResilienceHub,
			ScopeID:             "aws:123456789012:us-east-1",
			GenerationID:        "gen-1",
			CollectorInstanceID: "aws-prod",
			FencingToken:        1,
		},
	}
}

var _ resiliencehubservice.Client = (*Client)(nil)
