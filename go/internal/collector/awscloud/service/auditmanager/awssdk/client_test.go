// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsauditmanager "github.com/aws/aws-sdk-go-v2/service/auditmanager"
	awsauditmanagertypes "github.com/aws/aws-sdk-go-v2/service/auditmanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsAuditManagerMetadataOnly(t *testing.T) {
	assessmentARN := "arn:aws:auditmanager:us-east-1:123456789012:assessment/a1"
	frameworkARN := "arn:aws:auditmanager:us-east-1:123456789012:assessmentFramework/f1"
	controlARN := "arn:aws:auditmanager:us-east-1:123456789012:control/c1"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeAuditManagerAPI{
		status: awsauditmanagertypes.AccountStatusActive,
		assessmentPages: []*awsauditmanager.ListAssessmentsOutput{{
			AssessmentMetadata: []awsauditmanagertypes.AssessmentMetadataItem{{Id: aws.String("a1")}},
		}},
		assessments: map[string]*awsauditmanager.GetAssessmentOutput{
			"a1": {Assessment: &awsauditmanagertypes.Assessment{
				Arn:  aws.String(assessmentARN),
				Tags: map[string]string{"Environment": "prod"},
				Metadata: &awsauditmanagertypes.AssessmentMetadata{
					Id:             aws.String("a1"),
					Name:           aws.String("soc2"),
					ComplianceType: aws.String("SOC 2"),
					Status:         awsauditmanagertypes.AssessmentStatusActive,
					CreationTime:   aws.Time(createdAt),
					LastUpdated:    aws.Time(createdAt),
					AssessmentReportsDestination: &awsauditmanagertypes.AssessmentReportsDestination{
						Destination:     aws.String("s3://reports-bucket/exports"),
						DestinationType: awsauditmanagertypes.AssessmentReportDestinationTypeS3,
					},
					Scope: &awsauditmanagertypes.Scope{
						AwsAccounts: []awsauditmanagertypes.AWSAccount{{Id: aws.String("123456789012")}},
					},
				},
				Framework: &awsauditmanagertypes.AssessmentFramework{
					Arn: aws.String(frameworkARN),
					Id:  aws.String("f1"),
				},
			}},
		},
		frameworkPages: map[awsauditmanagertypes.FrameworkType][]*awsauditmanager.ListAssessmentFrameworksOutput{
			awsauditmanagertypes.FrameworkTypeStandard: {{
				FrameworkMetadataList: []awsauditmanagertypes.AssessmentFrameworkMetadata{{
					Arn:           aws.String(frameworkARN),
					Id:            aws.String("f1"),
					Name:          aws.String("SOC 2"),
					Type:          awsauditmanagertypes.FrameworkTypeStandard,
					ControlsCount: 61,
				}},
			}},
		},
		controlPages: map[awsauditmanagertypes.ControlType][]*awsauditmanager.ListControlsOutput{
			awsauditmanagertypes.ControlTypeStandard: {{
				ControlMetadataList: []awsauditmanagertypes.ControlMetadata{{
					Arn:            aws.String(controlARN),
					Id:             aws.String("c1"),
					Name:           aws.String("Logging enabled"),
					ControlSources: aws.String("AWS Config"),
				}},
			}},
		},
		settings: &awsauditmanagertypes.Settings{KmsKey: aws.String(kmsARN)},
	}

	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Assessments) != 1 {
		t.Fatalf("len(Assessments) = %d, want 1", len(snapshot.Assessments))
	}
	assessment := snapshot.Assessments[0]
	if assessment.ARN != assessmentARN {
		t.Fatalf("assessment ARN = %q, want %q", assessment.ARN, assessmentARN)
	}
	if assessment.FrameworkARN != frameworkARN {
		t.Fatalf("assessment FrameworkARN = %q, want %q", assessment.FrameworkARN, frameworkARN)
	}
	if assessment.ReportsS3Destination != "s3://reports-bucket/exports" {
		t.Fatalf("assessment ReportsS3Destination = %q", assessment.ReportsS3Destination)
	}
	if len(assessment.ScopeAccountIDs) != 1 || assessment.ScopeAccountIDs[0] != "123456789012" {
		t.Fatalf("assessment ScopeAccountIDs = %#v, want [123456789012]", assessment.ScopeAccountIDs)
	}
	if assessment.Tags["Environment"] != "prod" {
		t.Fatalf("assessment tag Environment = %q, want prod", assessment.Tags["Environment"])
	}
	if len(snapshot.Frameworks) != 1 || snapshot.Frameworks[0].ControlsCount != 61 {
		t.Fatalf("frameworks = %#v, want one framework with 61 controls", snapshot.Frameworks)
	}
	if len(snapshot.Controls) != 1 || snapshot.Controls[0].Type != "Standard" {
		t.Fatalf("controls = %#v, want one Standard control", snapshot.Controls)
	}
	if snapshot.KMSKeyARN != kmsARN {
		t.Fatalf("snapshot KMSKeyARN = %q, want %q", snapshot.KMSKeyARN, kmsARN)
	}
}

func TestClientReturnsWarningForUnregisteredAccount(t *testing.T) {
	api := &fakeAuditManagerAPI{status: awsauditmanagertypes.AccountStatusInactive}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil for unregistered account", err)
	}
	if len(snapshot.Assessments) != 0 || len(snapshot.Frameworks) != 0 || len(snapshot.Controls) != 0 {
		t.Fatalf("unregistered account returned resources: %#v", snapshot)
	}
	if len(snapshot.Warnings) != 1 || snapshot.Warnings[0].WarningKind != "auditmanager_not_registered" {
		t.Fatalf("warnings = %#v, want one auditmanager_not_registered warning", snapshot.Warnings)
	}
}

type fakeAuditManagerAPI struct {
	status          awsauditmanagertypes.AccountStatus
	assessmentPages []*awsauditmanager.ListAssessmentsOutput
	assessmentCall  int
	assessments     map[string]*awsauditmanager.GetAssessmentOutput
	frameworkPages  map[awsauditmanagertypes.FrameworkType][]*awsauditmanager.ListAssessmentFrameworksOutput
	frameworkCalls  map[awsauditmanagertypes.FrameworkType]int
	controlPages    map[awsauditmanagertypes.ControlType][]*awsauditmanager.ListControlsOutput
	controlCalls    map[awsauditmanagertypes.ControlType]int
	settings        *awsauditmanagertypes.Settings
}

func (f *fakeAuditManagerAPI) GetAccountStatus(
	context.Context,
	*awsauditmanager.GetAccountStatusInput,
	...func(*awsauditmanager.Options),
) (*awsauditmanager.GetAccountStatusOutput, error) {
	return &awsauditmanager.GetAccountStatusOutput{Status: f.status}, nil
}

func (f *fakeAuditManagerAPI) ListAssessments(
	context.Context,
	*awsauditmanager.ListAssessmentsInput,
	...func(*awsauditmanager.Options),
) (*awsauditmanager.ListAssessmentsOutput, error) {
	if f.assessmentCall >= len(f.assessmentPages) {
		return &awsauditmanager.ListAssessmentsOutput{}, nil
	}
	page := f.assessmentPages[f.assessmentCall]
	f.assessmentCall++
	return page, nil
}

func (f *fakeAuditManagerAPI) GetAssessment(
	_ context.Context,
	input *awsauditmanager.GetAssessmentInput,
	_ ...func(*awsauditmanager.Options),
) (*awsauditmanager.GetAssessmentOutput, error) {
	return f.assessments[aws.ToString(input.AssessmentId)], nil
}

func (f *fakeAuditManagerAPI) ListAssessmentFrameworks(
	_ context.Context,
	input *awsauditmanager.ListAssessmentFrameworksInput,
	_ ...func(*awsauditmanager.Options),
) (*awsauditmanager.ListAssessmentFrameworksOutput, error) {
	if f.frameworkCalls == nil {
		f.frameworkCalls = map[awsauditmanagertypes.FrameworkType]int{}
	}
	pages := f.frameworkPages[input.FrameworkType]
	idx := f.frameworkCalls[input.FrameworkType]
	if idx >= len(pages) {
		return &awsauditmanager.ListAssessmentFrameworksOutput{}, nil
	}
	f.frameworkCalls[input.FrameworkType] = idx + 1
	return pages[idx], nil
}

func (f *fakeAuditManagerAPI) ListControls(
	_ context.Context,
	input *awsauditmanager.ListControlsInput,
	_ ...func(*awsauditmanager.Options),
) (*awsauditmanager.ListControlsOutput, error) {
	if f.controlCalls == nil {
		f.controlCalls = map[awsauditmanagertypes.ControlType]int{}
	}
	pages := f.controlPages[input.ControlType]
	idx := f.controlCalls[input.ControlType]
	if idx >= len(pages) {
		return &awsauditmanager.ListControlsOutput{}, nil
	}
	f.controlCalls[input.ControlType] = idx + 1
	return pages[idx], nil
}

func (f *fakeAuditManagerAPI) GetSettings(
	context.Context,
	*awsauditmanager.GetSettingsInput,
	...func(*awsauditmanager.Options),
) (*awsauditmanager.GetSettingsOutput, error) {
	return &awsauditmanager.GetSettingsOutput{Settings: f.settings}, nil
}

func (f *fakeAuditManagerAPI) ListTagsForResource(
	context.Context,
	*awsauditmanager.ListTagsForResourceInput,
	...func(*awsauditmanager.Options),
) (*awsauditmanager.ListTagsForResourceOutput, error) {
	return &awsauditmanager.ListTagsForResourceOutput{}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceAuditManager,
	}
}
