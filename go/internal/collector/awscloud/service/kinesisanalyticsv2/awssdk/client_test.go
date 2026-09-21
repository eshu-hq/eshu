// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskav2 "github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2"
	awskav2types "github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsManagedFlinkMetadataOnly(t *testing.T) {
	appARN := "arn:aws:kinesisanalytics:us-east-1:123456789012:application/orders-flink"
	inputKDS := "arn:aws:kinesis:us-east-1:123456789012:stream/orders-in"
	outputFH := "arn:aws:firehose:us-east-1:123456789012:deliverystream/orders-fh-out"
	bucketARN := "arn:aws:s3:::orders-flink-code"
	roleARN := "arn:aws:iam::123456789012:role/orders-flink-role"
	logStreamARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/kinesis-analytics/orders-flink:log-stream:kinesis-analytics-log-stream"
	wantLogGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/kinesis-analytics/orders-flink"

	api := &fakeKAV2API{
		appPages: []*awskav2.ListApplicationsOutput{
			{
				ApplicationSummaries: []awskav2types.ApplicationSummary{{
					ApplicationName: aws.String("orders-flink"),
					ApplicationARN:  aws.String(appARN),
				}},
				NextToken: aws.String("page-2"),
			},
			{ApplicationSummaries: nil},
		},
		describe: map[string]*awskav2types.ApplicationDetail{
			"orders-flink": {
				ApplicationName:      aws.String("orders-flink"),
				ApplicationARN:       aws.String(appARN),
				ApplicationStatus:    awskav2types.ApplicationStatusRunning,
				RuntimeEnvironment:   awskav2types.RuntimeEnvironmentFlink118,
				ApplicationMode:      awskav2types.ApplicationModeStreaming,
				ApplicationVersionId: aws.Int64(7),
				ServiceExecutionRole: aws.String(roleARN),
				CloudWatchLoggingOptionDescriptions: []awskav2types.CloudWatchLoggingOptionDescription{{
					LogStreamARN: aws.String(logStreamARN),
				}},
				ApplicationConfigurationDescription: &awskav2types.ApplicationConfigurationDescription{
					ApplicationSnapshotConfigurationDescription: &awskav2types.ApplicationSnapshotConfigurationDescription{
						SnapshotsEnabled: aws.Bool(true),
					},
					ApplicationCodeConfigurationDescription: &awskav2types.ApplicationCodeConfigurationDescription{
						CodeContentType: awskav2types.CodeContentTypeZipfile,
						CodeContentDescription: &awskav2types.CodeContentDescription{
							TextContent: aws.String("SECRET FLINK JOB SQL SHOULD NOT LEAK"),
							S3ApplicationCodeLocationDescription: &awskav2types.S3ApplicationCodeLocationDescription{
								BucketARN: aws.String(bucketARN),
								FileKey:   aws.String("code/orders-flink.zip"),
							},
						},
					},
					FlinkApplicationConfigurationDescription: &awskav2types.FlinkApplicationConfigurationDescription{
						JobPlanDescription: aws.String("SECRET JOB PLAN SHOULD NOT LEAK"),
						ParallelismConfigurationDescription: &awskav2types.ParallelismConfigurationDescription{
							AutoScalingEnabled: aws.Bool(true),
							ConfigurationType:  awskav2types.ConfigurationTypeCustom,
							Parallelism:        aws.Int32(4),
							ParallelismPerKPU:  aws.Int32(2),
							CurrentParallelism: aws.Int32(4),
						},
					},
					SqlApplicationConfigurationDescription: &awskav2types.SqlApplicationConfigurationDescription{
						InputDescriptions: []awskav2types.InputDescription{{
							KinesisStreamsInputDescription: &awskav2types.KinesisStreamsInputDescription{
								ResourceARN: aws.String(inputKDS),
							},
						}},
						OutputDescriptions: []awskav2types.OutputDescription{{
							KinesisFirehoseOutputDescription: &awskav2types.KinesisFirehoseOutputDescription{
								ResourceARN: aws.String(outputFH),
							},
						}},
					},
					VpcConfigurationDescriptions: []awskav2types.VpcConfigurationDescription{{
						VpcConfigurationId: aws.String("1.1"),
						VpcId:              aws.String("vpc-0a1b2c3d"),
						SubnetIds:          []string{"subnet-0a1b2c3d"},
						SecurityGroupIds:   []string{"sg-0a1b2c3d"},
					}},
				},
			},
		},
		snapshots: map[string][]awskav2types.SnapshotDetails{
			"orders-flink": {{
				SnapshotName:         aws.String("snapshot-001"),
				SnapshotStatus:       awskav2types.SnapshotStatusReady,
				ApplicationVersionId: aws.Int64(6),
			}},
		},
		tags: map[string][]awskav2types.Tag{
			appARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	applications, err := client.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("ListApplications() error = %v, want nil", err)
	}
	if len(applications) != 1 {
		t.Fatalf("len(applications) = %d, want 1", len(applications))
	}
	app := applications[0]
	if app.ARN != appARN || app.Name != "orders-flink" {
		t.Fatalf("identity = %#v, want orders-flink/%s", app, appARN)
	}
	if app.Status != "RUNNING" || app.RuntimeEnvironment != "FLINK-1_18" || app.Mode != "STREAMING" {
		t.Fatalf("status/runtime/mode = %q/%q/%q", app.Status, app.RuntimeEnvironment, app.Mode)
	}
	if app.VersionID != 7 || app.ServiceExecutionRoleARN != roleARN {
		t.Fatalf("version/role = %d/%q", app.VersionID, app.ServiceExecutionRoleARN)
	}
	if !app.SnapshotsEnabled || !app.AutoScalingEnabled {
		t.Fatalf("posture = snapshots %v autoscale %v, want both true", app.SnapshotsEnabled, app.AutoScalingEnabled)
	}
	if app.ParallelismConfigurationType != "CUSTOM" || app.Parallelism != 4 || app.ParallelismPerKPU != 2 {
		t.Fatalf("parallelism = %#v", app)
	}
	if app.CodeContentType != "ZIPFILE" || app.CodeS3BucketARN != bucketARN || app.CodeS3FileKey != "code/orders-flink.zip" {
		t.Fatalf("code config = %#v", app)
	}
	if len(app.InputKinesisStreamARNs) != 1 || app.InputKinesisStreamARNs[0] != inputKDS {
		t.Fatalf("input KDS = %#v, want [%s]", app.InputKinesisStreamARNs, inputKDS)
	}
	if len(app.OutputFirehoseStreamARNs) != 1 || app.OutputFirehoseStreamARNs[0] != outputFH {
		t.Fatalf("output FH = %#v, want [%s]", app.OutputFirehoseStreamARNs, outputFH)
	}
	if len(app.VPCConfigurations) != 1 ||
		app.VPCConfigurations[0].SubnetIDs[0] != "subnet-0a1b2c3d" ||
		app.VPCConfigurations[0].SecurityGroupIDs[0] != "sg-0a1b2c3d" {
		t.Fatalf("vpc config = %#v", app.VPCConfigurations)
	}
	if len(app.LogGroupARNs) != 1 || app.LogGroupARNs[0] != wantLogGroupARN {
		t.Fatalf("log group ARNs = %#v, want [%s] (log stream ARN must be trimmed)", app.LogGroupARNs, wantLogGroupARN)
	}
	if len(app.Snapshots) != 1 || app.Snapshots[0].Name != "snapshot-001" || app.Snapshots[0].Status != "READY" {
		t.Fatalf("snapshots = %#v", app.Snapshots)
	}
	if app.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want Environment=prod", app.Tags)
	}
}

func TestLogGroupARNFromLogStreamARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:logs:us-east-1:1:log-group:/g:log-stream:s": "arn:aws:logs:us-east-1:1:log-group:/g",
		"arn:aws:logs:us-east-1:1:log-group:/g:log-stream:":  "arn:aws:logs:us-east-1:1:log-group:/g",
		"arn:aws:logs:us-east-1:1:log-group:/g:*":            "arn:aws:logs:us-east-1:1:log-group:/g",
		"arn:aws:logs:us-east-1:1:log-group:/g":              "arn:aws:logs:us-east-1:1:log-group:/g",
		"  arn:aws:logs:us-east-1:1:log-group:/g  ":          "arn:aws:logs:us-east-1:1:log-group:/g",
		"": "",
	}
	for input, want := range cases {
		if got := logGroupARNFromLogStreamARN(input); got != want {
			t.Fatalf("logGroupARNFromLogStreamARN(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClientReturnsNilForEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeKAV2API{appPages: []*awskav2.ListApplicationsOutput{{}}}, boundary: testBoundary()}
	applications, err := client.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("ListApplications() error = %v, want nil", err)
	}
	if applications != nil {
		t.Fatalf("ListApplications() = %#v, want nil for empty account", applications)
	}
}

type fakeKAV2API struct {
	appPages  []*awskav2.ListApplicationsOutput
	appCall   int
	describe  map[string]*awskav2types.ApplicationDetail
	snapshots map[string][]awskav2types.SnapshotDetails
	snapCalls map[string]int
	tags      map[string][]awskav2types.Tag
}

func (f *fakeKAV2API) ListApplications(
	_ context.Context,
	_ *awskav2.ListApplicationsInput,
	_ ...func(*awskav2.Options),
) (*awskav2.ListApplicationsOutput, error) {
	if f.appCall >= len(f.appPages) {
		return &awskav2.ListApplicationsOutput{}, nil
	}
	page := f.appPages[f.appCall]
	f.appCall++
	return page, nil
}

func (f *fakeKAV2API) DescribeApplication(
	_ context.Context,
	input *awskav2.DescribeApplicationInput,
	_ ...func(*awskav2.Options),
) (*awskav2.DescribeApplicationOutput, error) {
	return &awskav2.DescribeApplicationOutput{
		ApplicationDetail: f.describe[aws.ToString(input.ApplicationName)],
	}, nil
}

func (f *fakeKAV2API) ListApplicationSnapshots(
	_ context.Context,
	input *awskav2.ListApplicationSnapshotsInput,
	_ ...func(*awskav2.Options),
) (*awskav2.ListApplicationSnapshotsOutput, error) {
	if f.snapCalls == nil {
		f.snapCalls = map[string]int{}
	}
	name := aws.ToString(input.ApplicationName)
	if f.snapCalls[name] > 0 {
		return &awskav2.ListApplicationSnapshotsOutput{}, nil
	}
	f.snapCalls[name]++
	return &awskav2.ListApplicationSnapshotsOutput{SnapshotSummaries: f.snapshots[name]}, nil
}

func (f *fakeKAV2API) ListTagsForResource(
	_ context.Context,
	input *awskav2.ListTagsForResourceInput,
	_ ...func(*awskav2.Options),
) (*awskav2.ListTagsForResourceOutput, error) {
	return &awskav2.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceARN)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceKinesisAnalyticsV2,
	}
}
