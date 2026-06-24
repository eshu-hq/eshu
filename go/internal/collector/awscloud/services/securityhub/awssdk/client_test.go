// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecurityhub "github.com/aws/aws-sdk-go-v2/service/securityhub"
	awssecurityhubtypes "github.com/aws/aws-sdk-go-v2/service/securityhub/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestClientSnapshotReadsMetadataAndAggregatesFindingsWithoutLeakingPayloads(t *testing.T) {
	hubARN := "arn:aws:securityhub:us-east-1:123456789012:hub/default"
	standardARN := "arn:aws:securityhub:us-east-1::standards/aws-foundational-security-best-practices/v/1.0.0"
	subscriptionARN := "arn:aws:securityhub:us-east-1:123456789012:subscription/aws-foundational-security-best-practices/v/1.0.0"
	controlARN := "arn:aws:securityhub:us-east-1:123456789012:control/aws-foundational-security-best-practices/v/1.0.0/S3.1"
	actionARN := "arn:aws:securityhub:us-east-1:123456789012:action/custom/escalate"
	insightARN := "arn:aws:securityhub:us-east-1:123456789012:insight/custom/failed-controls"
	client := &fakeSecurityHubAPI{
		describeHubOutput: &awssecurityhub.DescribeHubOutput{
			AutoEnableControls:      aws.Bool(true),
			ControlFindingGenerator: awssecurityhubtypes.ControlFindingGeneratorSecurityControl,
			HubArn:                  aws.String(hubARN),
			SubscribedAt:            aws.String("2026-05-27T10:00:00Z"),
		},
		administratorOutput: &awssecurityhub.GetAdministratorAccountOutput{
			Administrator: &awssecurityhubtypes.Invitation{
				AccountId:    aws.String("999999999999"),
				MemberStatus: aws.String("Enabled"),
			},
		},
		memberPages: []*awssecurityhub.ListMembersOutput{{
			Members: []awssecurityhubtypes.Member{{
				AccountId:       aws.String("111122223333"),
				AdministratorId: aws.String("999999999999"),
				MemberStatus:    aws.String("Enabled"),
				InvitedAt:       aws.Time(time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)),
				UpdatedAt:       aws.Time(time.Date(2026, 5, 27, 11, 30, 0, 0, time.UTC)),
			}},
		}},
		standardPages: []*awssecurityhub.GetEnabledStandardsOutput{{
			StandardsSubscriptions: []awssecurityhubtypes.StandardsSubscription{{
				StandardsArn:               aws.String(standardARN),
				StandardsSubscriptionArn:   aws.String(subscriptionARN),
				StandardsStatus:            awssecurityhubtypes.StandardsStatusReady,
				StandardsControlsUpdatable: awssecurityhubtypes.StandardsControlsUpdatableReadyForUpdates,
				StandardsInput:             map[string]string{"regions": "us-east-1"},
				StandardsStatusReason: &awssecurityhubtypes.StandardsStatusReason{
					StatusReasonCode: awssecurityhubtypes.StatusReasonCodeInternalError,
				},
			}},
		}},
		controlPages: map[string][]*awssecurityhub.DescribeStandardsControlsOutput{
			subscriptionARN: {{
				Controls: []awssecurityhubtypes.StandardsControl{{
					ControlId:           aws.String("S3.1"),
					ControlStatus:       awssecurityhubtypes.ControlStatusEnabled,
					RelatedRequirements: []string{"CIS 2.1.2"},
					SeverityRating:      awssecurityhubtypes.SeverityRatingHigh,
					StandardsControlArn: aws.String(controlARN),
					Title:               aws.String("S3 Block Public Access setting should be enabled"),
				}},
			}},
		},
		actionTargetPages: []*awssecurityhub.DescribeActionTargetsOutput{{
			ActionTargets: []awssecurityhubtypes.ActionTarget{{
				ActionTargetArn: aws.String(actionARN),
				Description:     aws.String("page https://internal.example.invalid/hook?token=custom-action-secret"),
				Name:            aws.String("escalate"),
			}},
		}},
		insightPages: []*awssecurityhub.GetInsightsOutput{{
			Insights: []awssecurityhubtypes.Insight{{
				Filters: &awssecurityhubtypes.AwsSecurityFindingFilters{
					ResourceId: []awssecurityhubtypes.StringFilter{{
						Comparison: awssecurityhubtypes.StringFilterComparisonEquals,
						Value:      aws.String("arn:aws:s3:::private-bucket"),
					}},
				},
				GroupByAttribute: aws.String("ComplianceSecurityControlId"),
				InsightArn:       aws.String(insightARN),
				Name:             aws.String("Failed controls"),
			}},
		}},
		insightResults: map[string]*awssecurityhub.GetInsightResultsOutput{
			insightARN: {
				InsightResults: &awssecurityhubtypes.InsightResults{
					GroupByAttribute: aws.String("ComplianceSecurityControlId"),
					InsightArn:       aws.String(insightARN),
					ResultValues: []awssecurityhubtypes.InsightResultValue{{
						Count:                 aws.Int32(7),
						GroupByAttributeValue: aws.String("S3.1"),
					}},
				},
			},
		},
		findingPages: []*awssecurityhub.GetFindingsOutput{{
			Findings: []awssecurityhubtypes.AwsSecurityFinding{{
				AwsAccountId:  aws.String("123456789012"),
				CreatedAt:     aws.String("2026-05-27T12:00:00Z"),
				Description:   aws.String("attacker reached private instance"),
				GeneratorId:   aws.String("aws-foundational-security-best-practices/v/1.0.0/S3.1"),
				Id:            aws.String("finding-id-that-must-not-emit"),
				ProductArn:    aws.String("arn:aws:securityhub:us-east-1::product/aws/securityhub"),
				SchemaVersion: aws.String("2018-10-08"),
				Title:         aws.String("private finding title"),
				UpdatedAt:     aws.String("2026-05-27T12:05:00Z"),
				Compliance: &awssecurityhubtypes.Compliance{
					AssociatedStandards: []awssecurityhubtypes.AssociatedStandard{{
						StandardsId: aws.String("aws-foundational-security-best-practices/v/1.0.0"),
					}},
					SecurityControlId: aws.String("S3.1"),
					Status:            awssecurityhubtypes.ComplianceStatusFailed,
				},
				Network: &awssecurityhubtypes.Network{
					DestinationIpV4: aws.String("10.0.0.5"),
					Protocol:        aws.String("tcp"),
				},
				Note: &awssecurityhubtypes.Note{
					Text:      aws.String("do not page customer"),
					UpdatedAt: aws.String("2026-05-27T12:10:00Z"),
					UpdatedBy: aws.String("analyst@example.invalid"),
				},
				Process: &awssecurityhubtypes.ProcessDetails{
					Name: aws.String("terminate-process"),
					Path: aws.String("/opt/secret/agent"),
				},
				ProductFields: map[string]string{
					"token": "product-field-secret",
				},
				Remediation: &awssecurityhubtypes.Remediation{
					Recommendation: &awssecurityhubtypes.Recommendation{
						Text: aws.String("rotate the leaked secret"),
						Url:  aws.String("https://internal.example.invalid/remediate"),
					},
				},
				Resources: []awssecurityhubtypes.Resource{{
					Id:     aws.String("i-0abc123private"),
					Type:   aws.String("AwsEc2Instance"),
					Region: aws.String("us-east-1"),
					Tags:   map[string]string{"SecretTag": "finding-resource-tag-secret"},
				}},
				Severity: &awssecurityhubtypes.Severity{Label: awssecurityhubtypes.SeverityLabelHigh},
				UserDefinedFields: map[string]string{
					"owner": "user-defined-secret",
				},
				Workflow: &awssecurityhubtypes.Workflow{Status: awssecurityhubtypes.WorkflowStatusNew},
			}},
		}},
		tags: map[string]map[string]string{
			hubARN:          {"Environment": "prod"},
			subscriptionARN: {"Framework": "aws-foundational"},
		},
	}
	adapter := &Client{
		client:   client,
		boundary: testBoundary(),
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if snapshot.Hub.ARN != hubARN {
		t.Fatalf("Hub.ARN = %q, want %q", snapshot.Hub.ARN, hubARN)
	}
	if snapshot.Hub.Tags["Environment"] != "prod" {
		t.Fatalf("Hub.Tags = %#v, want Environment=prod", snapshot.Hub.Tags)
	}
	if got, want := len(snapshot.Members), 1; got != want {
		t.Fatalf("len(Members) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Standards), 1; got != want {
		t.Fatalf("len(Standards) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Standards[0].Controls), 1; got != want {
		t.Fatalf("len(Controls) = %d, want %d", got, want)
	}
	if got, want := snapshot.Standards[0].Controls[0].ComplianceCounts["FAILED"], int64(1); got != want {
		t.Fatalf("ComplianceCounts[FAILED] = %d, want %d", got, want)
	}
	if got, want := len(snapshot.FindingCounts), 1; got != want {
		t.Fatalf("len(FindingCounts) = %d, want %d", got, want)
	}
	if got, want := snapshot.FindingCounts[0].SeverityLabel, "HIGH"; got != want {
		t.Fatalf("FindingCounts[0].SeverityLabel = %q, want %q", got, want)
	}
	if got, want := snapshot.Insights[0].ControlIDs, []string{"S3.1"}; !stringSlicesEqual(got, want) {
		t.Fatalf("Insight.ControlIDs = %#v, want %#v", got, want)
	}
	if client.findingsCalls != 1 {
		t.Fatalf("GetFindings calls = %d, want 1", client.findingsCalls)
	}

	snapshotJSON := mustJSON(t, snapshot)
	for _, forbidden := range []string{
		"arn:aws:s3:::private-bucket",
		"i-0abc123private",
		"10.0.0.5",
		"terminate-process",
		"product-field-secret",
		"user-defined-secret",
		"rotate the leaked secret",
		"finding-resource-tag-secret",
		"do not page customer",
		"finding-id-that-must-not-emit",
	} {
		if strings.Contains(snapshotJSON, forbidden) {
			t.Fatalf("forbidden value %q leaked into snapshot: %s", forbidden, snapshotJSON)
		}
	}

	key, err := redact.NewKey([]byte("securityhub-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelopes, err := (securityhubservice.Scanner{Client: adapter, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	envelopeJSON := mustJSON(t, envelopes)
	for _, forbidden := range []string{
		"custom-action-secret",
		"arn:aws:s3:::private-bucket",
		"i-0abc123private",
		"10.0.0.5",
		"terminate-process",
		"product-field-secret",
		"user-defined-secret",
		"rotate the leaked secret",
		"finding-resource-tag-secret",
		"do not page customer",
	} {
		if strings.Contains(envelopeJSON, forbidden) {
			t.Fatalf("forbidden value %q leaked into emitted facts: %s", forbidden, envelopeJSON)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSecurityHub,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:securityhub:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(raw)
}

type fakeSecurityHubAPI struct {
	describeHubOutput *awssecurityhub.DescribeHubOutput

	administratorOutput *awssecurityhub.GetAdministratorAccountOutput

	memberPages []*awssecurityhub.ListMembersOutput
	memberCalls int

	standardPages []*awssecurityhub.GetEnabledStandardsOutput
	standardCalls int

	controlPages map[string][]*awssecurityhub.DescribeStandardsControlsOutput
	controlCalls map[string]int

	actionTargetPages []*awssecurityhub.DescribeActionTargetsOutput
	actionTargetCalls int

	insightPages []*awssecurityhub.GetInsightsOutput
	insightCalls int

	insightResults map[string]*awssecurityhub.GetInsightResultsOutput

	findingPages   []*awssecurityhub.GetFindingsOutput
	findingsCalls  int
	lastFindingsIn *awssecurityhub.GetFindingsInput

	tags map[string]map[string]string
}

func (f *fakeSecurityHubAPI) DescribeHub(
	context.Context,
	*awssecurityhub.DescribeHubInput,
	...func(*awssecurityhub.Options),
) (*awssecurityhub.DescribeHubOutput, error) {
	return f.describeHubOutput, nil
}

func (f *fakeSecurityHubAPI) GetAdministratorAccount(
	context.Context,
	*awssecurityhub.GetAdministratorAccountInput,
	...func(*awssecurityhub.Options),
) (*awssecurityhub.GetAdministratorAccountOutput, error) {
	return f.administratorOutput, nil
}

func (f *fakeSecurityHubAPI) ListMembers(
	_ context.Context,
	_ *awssecurityhub.ListMembersInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.ListMembersOutput, error) {
	if f.memberCalls >= len(f.memberPages) {
		return &awssecurityhub.ListMembersOutput{}, nil
	}
	page := f.memberPages[f.memberCalls]
	f.memberCalls++
	return page, nil
}

func (f *fakeSecurityHubAPI) GetEnabledStandards(
	_ context.Context,
	_ *awssecurityhub.GetEnabledStandardsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.GetEnabledStandardsOutput, error) {
	if f.standardCalls >= len(f.standardPages) {
		return &awssecurityhub.GetEnabledStandardsOutput{}, nil
	}
	page := f.standardPages[f.standardCalls]
	f.standardCalls++
	return page, nil
}

func (f *fakeSecurityHubAPI) DescribeStandardsControls(
	_ context.Context,
	input *awssecurityhub.DescribeStandardsControlsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.DescribeStandardsControlsOutput, error) {
	subscriptionARN := aws.ToString(input.StandardsSubscriptionArn)
	if f.controlCalls == nil {
		f.controlCalls = make(map[string]int)
	}
	pages := f.controlPages[subscriptionARN]
	call := f.controlCalls[subscriptionARN]
	if call >= len(pages) {
		return &awssecurityhub.DescribeStandardsControlsOutput{}, nil
	}
	f.controlCalls[subscriptionARN] = call + 1
	return pages[call], nil
}

func (f *fakeSecurityHubAPI) DescribeActionTargets(
	_ context.Context,
	_ *awssecurityhub.DescribeActionTargetsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.DescribeActionTargetsOutput, error) {
	if f.actionTargetCalls >= len(f.actionTargetPages) {
		return &awssecurityhub.DescribeActionTargetsOutput{}, nil
	}
	page := f.actionTargetPages[f.actionTargetCalls]
	f.actionTargetCalls++
	return page, nil
}

func (f *fakeSecurityHubAPI) GetInsights(
	_ context.Context,
	_ *awssecurityhub.GetInsightsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.GetInsightsOutput, error) {
	if f.insightCalls >= len(f.insightPages) {
		return &awssecurityhub.GetInsightsOutput{}, nil
	}
	page := f.insightPages[f.insightCalls]
	f.insightCalls++
	return page, nil
}

func (f *fakeSecurityHubAPI) GetInsightResults(
	_ context.Context,
	input *awssecurityhub.GetInsightResultsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.GetInsightResultsOutput, error) {
	return f.insightResults[aws.ToString(input.InsightArn)], nil
}

func (f *fakeSecurityHubAPI) GetFindings(
	_ context.Context,
	input *awssecurityhub.GetFindingsInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.GetFindingsOutput, error) {
	f.lastFindingsIn = input
	if f.findingsCalls >= len(f.findingPages) {
		return &awssecurityhub.GetFindingsOutput{}, nil
	}
	page := f.findingPages[f.findingsCalls]
	f.findingsCalls++
	return page, nil
}

func (f *fakeSecurityHubAPI) ListTagsForResource(
	_ context.Context,
	input *awssecurityhub.ListTagsForResourceInput,
	_ ...func(*awssecurityhub.Options),
) (*awssecurityhub.ListTagsForResourceOutput, error) {
	return &awssecurityhub.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

var _ apiClient = (*fakeSecurityHubAPI)(nil)
