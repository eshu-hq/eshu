// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfis "github.com/aws/aws-sdk-go-v2/service/fis"
	awsfistypes "github.com/aws/aws-sdk-go-v2/service/fis/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
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
				Id:  aws.String("EXTabc"),
				Arn: aws.String(templateARN),
			}},
		}},
		templates: map[string]*awsfistypes.ExperimentTemplate{
			"EXTabc": {
				Id:           aws.String("EXTabc"),
				Arn:          aws.String(templateARN),
				Description:  aws.String("stop fault"),
				RoleArn:      aws.String(roleARN),
				CreationTime: aws.Time(createdAt),
				Actions: map[string]awsfistypes.ExperimentTemplateAction{
					"stop": {
						ActionId:    aws.String("aws:ec2:stop-instances"),
						Description: aws.String("stop"),
						// Parameters must never surface in scanner metadata.
						Parameters: map[string]string{"startInstancesAfterDuration": "PT5M"},
					},
				},
				Targets: map[string]awsfistypes.ExperimentTemplateTarget{
					"inst": {
						ResourceType:  aws.String("aws:ec2:instance"),
						SelectionMode: aws.String("ALL"),
						ResourceArns:  []string{instanceARN},
						// Filters and resource tags must never surface.
						ResourceTags: map[string]string{"env": "prod"},
					},
				},
				LogConfiguration: &awsfistypes.ExperimentTemplateLogConfiguration{
					CloudWatchLogsConfiguration: &awsfistypes.ExperimentTemplateCloudWatchLogsLogConfiguration{
						LogGroupArn: aws.String(logGroupARN),
					},
					S3Configuration: &awsfistypes.ExperimentTemplateS3LogConfiguration{
						BucketName: aws.String("fis-logs"),
						Prefix:     aws.String("exp/"),
					},
				},
				StopConditions: []awsfistypes.ExperimentTemplateStopCondition{{
					Source: aws.String("aws:cloudwatch:alarm"),
					Value:  aws.String(alarmARN),
				}, {
					Source: aws.String("none"),
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
		ExperimentTemplate: f.templates[aws.ToString(input.Id)],
	}, nil
}

func (f *fakeFISAPI) ListTagsForResource(
	_ context.Context,
	input *awsfis.ListTagsForResourceInput,
	_ ...func(*awsfis.Options),
) (*awsfis.ListTagsForResourceOutput, error) {
	return &awsfis.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceFIS,
	}
}
