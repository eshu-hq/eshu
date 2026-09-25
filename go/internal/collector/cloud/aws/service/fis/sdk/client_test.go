// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsfis "github.com/aws/aws-sdk-go-v2/service/fis"
	awsfistypes "github.com/aws/aws-sdk-go-v2/service/fis/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsTemplateMetadataOnly(t *testing.T) {
	templateARN := "arn:aws:fis:us-east-1:123456789012:experiment-template/EXTabc"
	roleARN := "arn:aws:iam::123456789012:role/fis-exec"
	instanceARN := "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/fis:*"
	alarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:abort"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeFISAPI{
		listPages: []*awsfis.ListExperimentTemplatesOutput{{
			ExperimentTemplates: []awsfistypes.ExperimentTemplateSummary{{
				Id:  awsv2.String("EXTabc"),
				Arn: awsv2.String(templateARN),
			}},
		}},
		templates: map[string]*awsfistypes.ExperimentTemplate{
			"EXTabc": {
				Id:           awsv2.String("EXTabc"),
				Arn:          awsv2.String(templateARN),
				Description:  awsv2.String("stop fault"),
				RoleArn:      awsv2.String(roleARN),
				CreationTime: awsv2.Time(createdAt),
				Actions: map[string]awsfistypes.ExperimentTemplateAction{
					"stop": {
						ActionId:    awsv2.String("aws:ec2:stop-instances"),
						Description: awsv2.String("stop"),
						// Parameters must never surface in scanner metadata.
						Parameters: map[string]string{"startInstancesAfterDuration": "PT5M"},
					},
				},
				Targets: map[string]awsfistypes.ExperimentTemplateTarget{
					"inst": {
						ResourceType:  awsv2.String("aws:ec2:instance"),
						SelectionMode: awsv2.String("ALL"),
						ResourceArns:  []string{instanceARN},
						// Filters and resource tags must never surface.
						ResourceTags: map[string]string{"env": "prod"},
					},
				},
				LogConfiguration: &awsfistypes.ExperimentTemplateLogConfiguration{
					CloudWatchLogsConfiguration: &awsfistypes.ExperimentTemplateCloudWatchLogsLogConfiguration{
						LogGroupArn: awsv2.String(logGroupARN),
					},
					S3Configuration: &awsfistypes.ExperimentTemplateS3LogConfiguration{
						BucketName: awsv2.String("fis-logs"),
						Prefix:     awsv2.String("exp/"),
					},
				},
				StopConditions: []awsfistypes.ExperimentTemplateStopCondition{{
					Source: awsv2.String("aws:cloudwatch:alarm"),
					Value:  awsv2.String(alarmARN),
				}, {
					Source: awsv2.String("none"),
				}},
				Tags: map[string]string{"Name": "stop-prod"},
			},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Templates) != 1 {
		t.Fatalf("len(Templates) = %d, want 1", len(snapshot.Templates))
	}
	template := snapshot.Templates[0]
	if template.ARN != templateARN {
		t.Fatalf("template ARN = %q, want %q", template.ARN, templateARN)
	}
	if template.Name != "stop-prod" {
		t.Fatalf("template Name = %q, want stop-prod (from Name tag)", template.Name)
	}
	if template.RoleARN != roleARN {
		t.Fatalf("template RoleARN = %q, want %q", template.RoleARN, roleARN)
	}
	if len(template.Actions) != 1 || template.Actions[0].ActionID != "aws:ec2:stop-instances" {
		t.Fatalf("Actions = %#v, want one aws:ec2:stop-instances", template.Actions)
	}
	if len(template.Targets) != 1 || len(template.Targets[0].ResourceARNs) != 1 {
		t.Fatalf("Targets = %#v, want one target with one ARN", template.Targets)
	}
	if template.LogGroupARN != logGroupARN {
		t.Fatalf("LogGroupARN = %q, want %q", template.LogGroupARN, logGroupARN)
	}
	if template.LogS3Bucket != "fis-logs" || template.LogS3Prefix != "exp/" {
		t.Fatalf("S3 log destination = %q/%q, want fis-logs/exp/", template.LogS3Bucket, template.LogS3Prefix)
	}
	if len(template.StopConditionAlarmARNs) != 1 || template.StopConditionAlarmARNs[0] != alarmARN {
		t.Fatalf("StopConditionAlarmARNs = %#v, want [%q]", template.StopConditionAlarmARNs, alarmARN)
	}
}

type fakeFISAPI struct {
	listPages []*awsfis.ListExperimentTemplatesOutput
	listCall  int
	templates map[string]*awsfistypes.ExperimentTemplate
	tags      map[string]map[string]string
}

func (f *fakeFISAPI) ListExperimentTemplates(
	_ context.Context,
	_ *awsfis.ListExperimentTemplatesInput,
	_ ...func(*awsfis.Options),
) (*awsfis.ListExperimentTemplatesOutput, error) {
	if f.listCall >= len(f.listPages) {
		return &awsfis.ListExperimentTemplatesOutput{}, nil
	}
	page := f.listPages[f.listCall]
	f.listCall++
	return page, nil
}

func (f *fakeFISAPI) GetExperimentTemplate(
	_ context.Context,
	input *awsfis.GetExperimentTemplateInput,
	_ ...func(*awsfis.Options),
) (*awsfis.GetExperimentTemplateOutput, error) {
	return &awsfis.GetExperimentTemplateOutput{
		ExperimentTemplate: f.templates[awsv2.ToString(input.Id)],
	}, nil
}

func (f *fakeFISAPI) ListTagsForResource(
	_ context.Context,
	input *awsfis.ListTagsForResourceInput,
	_ ...func(*awsfis.Options),
) (*awsfis.ListTagsForResourceOutput, error) {
	return &awsfis.ListTagsForResourceOutput{
		Tags: f.tags[awsv2.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceFIS,
	}
}
