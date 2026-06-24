// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	awscwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatch"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the narrow CloudWatch SDK surface the adapter is allowed to
// call. It intentionally excludes GetDashboard (dashboard body JSON) and
// every mutation API (PutMetricAlarm, DeleteAlarms, PutCompositeAlarm,
// PutDashboard, DeleteDashboards, EnableAlarmActions, DisableAlarmActions,
// SetAlarmState, PutInsightRule, DeleteInsightRules, StartMetricStreams,
// StopMetricStreams, PutMetricData). Because the Client struct holds an
// apiClient value rather than the concrete *cloudwatch.Client, those methods
// are unreachable from this package.
type apiClient interface {
	DescribeAlarms(
		context.Context,
		*awscw.DescribeAlarmsInput,
		...func(*awscw.Options),
	) (*awscw.DescribeAlarmsOutput, error)
	ListDashboards(
		context.Context,
		*awscw.ListDashboardsInput,
		...func(*awscw.Options),
	) (*awscw.ListDashboardsOutput, error)
	DescribeInsightRules(
		context.Context,
		*awscw.DescribeInsightRulesInput,
		...func(*awscw.Options),
	) (*awscw.DescribeInsightRulesOutput, error)
	ListMetricStreams(
		context.Context,
		*awscw.ListMetricStreamsInput,
		...func(*awscw.Options),
	) (*awscw.ListMetricStreamsOutput, error)
	GetMetricStream(
		context.Context,
		*awscw.GetMetricStreamInput,
		...func(*awscw.Options),
	) (*awscw.GetMetricStreamOutput, error)
	ListTagsForResource(
		context.Context,
		*awscw.ListTagsForResourceInput,
		...func(*awscw.Options),
	) (*awscw.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CloudWatch control-plane calls into metadata-only
// scanner records. It never calls GetDashboard, PutMetricData, or any
// mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudWatch SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscw.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListMetricAlarms returns metric alarm metadata visible to the configured
// AWS credentials.
func (c *Client) ListMetricAlarms(ctx context.Context) ([]cwservice.MetricAlarm, error) {
	var alarms []cwservice.MetricAlarm
	var nextToken *string
	for {
		var page *awscw.DescribeAlarmsOutput
		err := c.recordAPICall(ctx, "DescribeAlarms.metric", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeAlarms(callCtx, &awscw.DescribeAlarmsInput{
				AlarmTypes: []awscwtypes.AlarmType{awscwtypes.AlarmTypeMetricAlarm},
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return alarms, nil
		}
		for _, raw := range page.MetricAlarms {
			tags, err := c.listTags(ctx, aws.ToString(raw.AlarmArn))
			if err != nil {
				return nil, err
			}
			alarms = append(alarms, mapMetricAlarm(raw, tags))
		}
		if aws.ToString(page.NextToken) == "" {
			return alarms, nil
		}
		nextToken = page.NextToken
	}
}

// ListCompositeAlarms returns composite alarm metadata visible to the
// configured AWS credentials.
func (c *Client) ListCompositeAlarms(ctx context.Context) ([]cwservice.CompositeAlarm, error) {
	var alarms []cwservice.CompositeAlarm
	var nextToken *string
	for {
		var page *awscw.DescribeAlarmsOutput
		err := c.recordAPICall(ctx, "DescribeAlarms.composite", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeAlarms(callCtx, &awscw.DescribeAlarmsInput{
				AlarmTypes: []awscwtypes.AlarmType{awscwtypes.AlarmTypeCompositeAlarm},
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return alarms, nil
		}
		for _, raw := range page.CompositeAlarms {
			tags, err := c.listTags(ctx, aws.ToString(raw.AlarmArn))
			if err != nil {
				return nil, err
			}
			alarms = append(alarms, mapCompositeAlarm(raw, tags))
		}
		if aws.ToString(page.NextToken) == "" {
			return alarms, nil
		}
		nextToken = page.NextToken
	}
}

// ListDashboards returns dashboard identity (name, ARN, last modified, size)
// visible to the configured AWS credentials. The dashboard body JSON is
// never read because the adapter's apiClient interface does not include
// GetDashboard.
func (c *Client) ListDashboards(ctx context.Context) ([]cwservice.Dashboard, error) {
	var dashboards []cwservice.Dashboard
	var nextToken *string
	for {
		var page *awscw.ListDashboardsOutput
		err := c.recordAPICall(ctx, "ListDashboards", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDashboards(callCtx, &awscw.ListDashboardsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dashboards, nil
		}
		for _, raw := range page.DashboardEntries {
			dashboards = append(dashboards, mapDashboard(raw))
		}
		if aws.ToString(page.NextToken) == "" {
			return dashboards, nil
		}
		nextToken = page.NextToken
	}
}

// ListInsightRules returns Contributor Insights rule identity (name, state,
// schema) visible to the configured AWS credentials. The rule definition
// body is not mapped onto the scanner-owned model.
func (c *Client) ListInsightRules(ctx context.Context) ([]cwservice.InsightRule, error) {
	var rules []cwservice.InsightRule
	var nextToken *string
	for {
		var page *awscw.DescribeInsightRulesOutput
		err := c.recordAPICall(ctx, "DescribeInsightRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeInsightRules(callCtx, &awscw.DescribeInsightRulesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, raw := range page.InsightRules {
			rules = append(rules, mapInsightRule(raw))
		}
		if aws.ToString(page.NextToken) == "" {
			return rules, nil
		}
		nextToken = page.NextToken
	}
}

// ListMetricStreams returns metric stream metadata visible to the configured
// AWS credentials.
func (c *Client) ListMetricStreams(ctx context.Context) ([]cwservice.MetricStream, error) {
	var streams []cwservice.MetricStream
	var nextToken *string
	for {
		var page *awscw.ListMetricStreamsOutput
		err := c.recordAPICall(ctx, "ListMetricStreams", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMetricStreams(callCtx, &awscw.ListMetricStreamsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return streams, nil
		}
		for _, entry := range page.Entries {
			details, err := c.getMetricStream(ctx, aws.ToString(entry.Name))
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(entry.Arn))
			if err != nil {
				return nil, err
			}
			streams = append(streams, mapMetricStream(entry, details, tags))
		}
		if aws.ToString(page.NextToken) == "" {
			return streams, nil
		}
		nextToken = page.NextToken
	}
}

func (c *Client) getMetricStream(ctx context.Context, name string) (*awscw.GetMetricStreamOutput, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	var output *awscw.GetMetricStreamOutput
	err := c.recordAPICall(ctx, "GetMetricStream", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetMetricStream(callCtx, &awscw.GetMetricStreamInput{
			Name: aws.String(name),
		})
		return err
	})
	if isThrottleError(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awscw.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awscw.ListTagsForResourceInput{
			ResourceARN: aws.String(resourceARN),
		})
		return err
	})
	if isThrottleError(err) {
		return nil, nil
	}
	if err != nil || output == nil {
		return nil, err
	}
	return cloneTags(output.Tags), nil
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

func cloneTags(tags []awscwtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

var _ cwservice.Client = (*Client)(nil)

var _ apiClient = (*awscw.Client)(nil)
