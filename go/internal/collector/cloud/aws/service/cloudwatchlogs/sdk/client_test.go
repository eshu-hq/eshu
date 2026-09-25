// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"slices"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	awscloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsCloudWatchLogsMetadataOnly(t *testing.T) {
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/orders"
	wildcardARN := logGroupARN + ":*"
	fallbackARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/ecs/payments:*"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/logs"
	createdAtMillis := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC).UnixMilli()
	api := &fakeCloudWatchLogsAPI{
		groupPages: []*awscloudwatchlogs.DescribeLogGroupsOutput{{
			LogGroups: []awscloudwatchlogstypes.LogGroup{{
				Arn:                              awsv2.String(wildcardARN),
				LogGroupArn:                      awsv2.String(logGroupARN),
				LogGroupName:                     awsv2.String("/aws/lambda/orders"),
				CreationTime:                     awsv2.Int64(createdAtMillis),
				RetentionInDays:                  awsv2.Int32(30),
				StoredBytes:                      awsv2.Int64(2048),
				MetricFilterCount:                awsv2.Int32(2),
				LogGroupClass:                    awscloudwatchlogstypes.LogGroupClassStandard,
				DataProtectionStatus:             awscloudwatchlogstypes.DataProtectionStatusActivated,
				DeletionProtectionEnabled:        awsv2.Bool(true),
				BearerTokenAuthenticationEnabled: awsv2.Bool(true),
				InheritedProperties: []awscloudwatchlogstypes.InheritedProperty{
					awscloudwatchlogstypes.InheritedPropertyAccountDataProtection,
				},
				KmsKeyId: awsv2.String(kmsARN),
			}},
			NextToken: awsv2.String("groups-next"),
		}, {
			LogGroups: []awscloudwatchlogstypes.LogGroup{{
				Arn:          awsv2.String(fallbackARN),
				LogGroupName: awsv2.String("/aws/ecs/payments"),
			}},
		}},
		tags: map[string]*awscloudwatchlogs.ListTagsForResourceOutput{
			logGroupARN: {
				Tags: map[string]string{"Environment": "prod"},
			},
			"arn:aws:logs:us-east-1:123456789012:log-group:/aws/ecs/payments": {
				Tags: map[string]string{"Service": "payments"},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	logGroups, err := adapter.ListLogGroups(context.Background())
	if err != nil {
		t.Fatalf("ListLogGroups() error = %v, want nil", err)
	}

	if got, want := len(logGroups), 2; got != want {
		t.Fatalf("len(logGroups) = %d, want %d", got, want)
	}
	if got, want := api.groupLimits, []int32{50, 50}; !slices.Equal(got, want) {
		t.Fatalf("DescribeLogGroups Limit = %#v, want %#v", got, want)
	}
	if got, want := api.groupTokens, []string{"", "groups-next"}; !slices.Equal(got, want) {
		t.Fatalf("DescribeLogGroups NextToken = %#v, want %#v", got, want)
	}
	if got, want := api.tagARNs, []string{
		logGroupARN,
		"arn:aws:logs:us-east-1:123456789012:log-group:/aws/ecs/payments",
	}; !slices.Equal(got, want) {
		t.Fatalf("ListTagsForResource ResourceArn = %#v, want %#v", got, want)
	}
	orders := logGroups[0]
	if orders.ARN != logGroupARN || orders.Name != "/aws/lambda/orders" {
		t.Fatalf("orders identity = %#v, want non-wildcard ARN and name", orders)
	}
	if orders.CreationTime != time.UnixMilli(createdAtMillis).UTC() {
		t.Fatalf("creation time = %s, want %s", orders.CreationTime, time.UnixMilli(createdAtMillis).UTC())
	}
	if orders.RetentionInDays != 30 || orders.StoredBytes != 2048 || orders.MetricFilterCount != 2 {
		t.Fatalf("orders counters = %#v, want retention/stored bytes/filter count", orders)
	}
	if orders.LogGroupClass != "STANDARD" || orders.DataProtectionStatus != "ACTIVATED" {
		t.Fatalf("orders class/protection = %#v, want mapped enum values", orders)
	}
	if !slices.Equal(orders.InheritedProperties, []string{"ACCOUNT_DATA_PROTECTION"}) {
		t.Fatalf("inherited properties = %#v, want account data protection", orders.InheritedProperties)
	}
	if orders.KMSKeyID != kmsARN || orders.Tags["Environment"] != "prod" {
		t.Fatalf("orders encryption/tags = %#v, want KMS and tags", orders)
	}
	if !orders.DeletionProtected || !orders.BearerTokenAuth {
		t.Fatalf("orders protection flags = %#v, want deletion and bearer-token auth enabled", orders)
	}
	if got, want := logGroups[1].ARN, "arn:aws:logs:us-east-1:123456789012:log-group:/aws/ecs/payments"; got != want {
		t.Fatalf("fallback ARN = %q, want trimmed wildcard ARN %q", got, want)
	}
}

func TestClientListsLogGroupsWhenTagReadIsThrottled(t *testing.T) {
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/orders"
	api := &fakeCloudWatchLogsAPI{
		groupPages: []*awscloudwatchlogs.DescribeLogGroupsOutput{{
			LogGroups: []awscloudwatchlogstypes.LogGroup{{
				LogGroupArn:  awsv2.String(logGroupARN),
				LogGroupName: awsv2.String("/aws/lambda/orders"),
			}},
		}},
		tagErrors: map[string]error{
			logGroupARN: &smithy.GenericAPIError{
				Code:    "ThrottlingException",
				Message: "Rate exceeded",
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	logGroups, err := adapter.ListLogGroups(context.Background())
	if err != nil {
		t.Fatalf("ListLogGroups() error = %v, want nil", err)
	}
	if got, want := len(logGroups), 1; got != want {
		t.Fatalf("len(logGroups) = %d, want %d", got, want)
	}
	if len(logGroups[0].Tags) != 0 {
		t.Fatalf("Tags = %#v, want no tags when tag read is throttled", logGroups[0].Tags)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceCloudWatchLogs,
	}
}

type fakeCloudWatchLogsAPI struct {
	groupPages  []*awscloudwatchlogs.DescribeLogGroupsOutput
	groupCalls  int
	groupLimits []int32
	groupTokens []string
	tags        map[string]*awscloudwatchlogs.ListTagsForResourceOutput
	tagErrors   map[string]error
	tagARNs     []string
}

func (f *fakeCloudWatchLogsAPI) DescribeLogGroups(
	_ context.Context,
	input *awscloudwatchlogs.DescribeLogGroupsInput,
	_ ...func(*awscloudwatchlogs.Options),
) (*awscloudwatchlogs.DescribeLogGroupsOutput, error) {
	f.groupLimits = append(f.groupLimits, awsv2.ToInt32(input.Limit))
	f.groupTokens = append(f.groupTokens, awsv2.ToString(input.NextToken))
	if f.groupCalls >= len(f.groupPages) {
		return &awscloudwatchlogs.DescribeLogGroupsOutput{}, nil
	}
	page := f.groupPages[f.groupCalls]
	f.groupCalls++
	return page, nil
}

func (f *fakeCloudWatchLogsAPI) ListTagsForResource(
	_ context.Context,
	input *awscloudwatchlogs.ListTagsForResourceInput,
	_ ...func(*awscloudwatchlogs.Options),
) (*awscloudwatchlogs.ListTagsForResourceOutput, error) {
	resourceARN := awsv2.ToString(input.ResourceArn)
	f.tagARNs = append(f.tagARNs, resourceARN)
	if err := f.tagErrors[resourceARN]; err != nil {
		return nil, err
	}
	if output := f.tags[resourceARN]; output != nil {
		return output, nil
	}
	return &awscloudwatchlogs.ListTagsForResourceOutput{}, nil
}
