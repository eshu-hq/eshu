// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsautoscaling "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	autoscalingservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/autoscaling"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only AWS EC2 Auto Scaling read surface used by the
// adapter. Only Describe reads appear here; no CreateAutoScalingGroup,
// UpdateAutoScalingGroup, DeleteAutoScalingGroup, SetDesiredCapacity,
// TerminateInstanceInAutoScalingGroup, or any Create/Update/Delete/Set/Put
// operation is reachable.
type apiClient interface {
	DescribeAutoScalingGroups(context.Context, *awsautoscaling.DescribeAutoScalingGroupsInput, ...func(*awsautoscaling.Options)) (*awsautoscaling.DescribeAutoScalingGroupsOutput, error)
	DescribeLaunchConfigurations(context.Context, *awsautoscaling.DescribeLaunchConfigurationsInput, ...func(*awsautoscaling.Options)) (*awsautoscaling.DescribeLaunchConfigurationsOutput, error)
	DescribePolicies(context.Context, *awsautoscaling.DescribePoliciesInput, ...func(*awsautoscaling.Options)) (*awsautoscaling.DescribePoliciesOutput, error)
	DescribeScheduledActions(context.Context, *awsautoscaling.DescribeScheduledActionsInput, ...func(*awsautoscaling.Options)) (*awsautoscaling.DescribeScheduledActionsOutput, error)
	DescribeLifecycleHooks(context.Context, *awsautoscaling.DescribeLifecycleHooksInput, ...func(*awsautoscaling.Options)) (*awsautoscaling.DescribeLifecycleHooksOutput, error)
}

// Client adapts the AWS SDK for Go v2 Auto Scaling client into scanner-owned
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Auto Scaling SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsautoscaling.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListAutoScalingGroups returns all Auto Scaling groups visible to the
// configured AWS credentials.
func (c *Client) ListAutoScalingGroups(ctx context.Context) ([]autoscalingservice.Group, error) {
	paginator := awsautoscaling.NewDescribeAutoScalingGroupsPaginator(c.client, &awsautoscaling.DescribeAutoScalingGroupsInput{})
	var groups []autoscalingservice.Group
	for paginator.HasMorePages() {
		var page *awsautoscaling.DescribeAutoScalingGroupsOutput
		err := c.recordAPICall(ctx, "DescribeAutoScalingGroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.AutoScalingGroups {
			groups = append(groups, mapGroup(detail))
		}
	}
	return groups, nil
}

// ListLaunchConfigurations returns all Auto Scaling launch configurations
// visible to the configured AWS credentials. The adapter maps identity only; it
// never reads launch configuration UserData.
func (c *Client) ListLaunchConfigurations(ctx context.Context) ([]autoscalingservice.LaunchConfiguration, error) {
	paginator := awsautoscaling.NewDescribeLaunchConfigurationsPaginator(c.client, &awsautoscaling.DescribeLaunchConfigurationsInput{})
	var launchConfigurations []autoscalingservice.LaunchConfiguration
	for paginator.HasMorePages() {
		var page *awsautoscaling.DescribeLaunchConfigurationsOutput
		err := c.recordAPICall(ctx, "DescribeLaunchConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.LaunchConfigurations {
			launchConfigurations = append(launchConfigurations, mapLaunchConfiguration(detail))
		}
	}
	return launchConfigurations, nil
}

// ListScalingPolicies returns all Auto Scaling scaling policies visible to the
// configured AWS credentials.
func (c *Client) ListScalingPolicies(ctx context.Context) ([]autoscalingservice.ScalingPolicy, error) {
	paginator := awsautoscaling.NewDescribePoliciesPaginator(c.client, &awsautoscaling.DescribePoliciesInput{})
	var policies []autoscalingservice.ScalingPolicy
	for paginator.HasMorePages() {
		var page *awsautoscaling.DescribePoliciesOutput
		err := c.recordAPICall(ctx, "DescribePolicies", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.ScalingPolicies {
			policies = append(policies, mapScalingPolicy(detail))
		}
	}
	return policies, nil
}

// ListScheduledActions returns all Auto Scaling scheduled actions visible to
// the configured AWS credentials.
func (c *Client) ListScheduledActions(ctx context.Context) ([]autoscalingservice.ScheduledAction, error) {
	paginator := awsautoscaling.NewDescribeScheduledActionsPaginator(c.client, &awsautoscaling.DescribeScheduledActionsInput{})
	var scheduledActions []autoscalingservice.ScheduledAction
	for paginator.HasMorePages() {
		var page *awsautoscaling.DescribeScheduledActionsOutput
		err := c.recordAPICall(ctx, "DescribeScheduledActions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.ScheduledUpdateGroupActions {
			scheduledActions = append(scheduledActions, mapScheduledAction(detail))
		}
	}
	return scheduledActions, nil
}

// ListLifecycleHooks returns the lifecycle hooks defined on one Auto Scaling
// group. DescribeLifecycleHooks is not paginated by AWS; it returns all hooks
// for the named group in a single call.
func (c *Client) ListLifecycleHooks(ctx context.Context, group autoscalingservice.Group) ([]autoscalingservice.LifecycleHook, error) {
	groupName := strings.TrimSpace(group.Name)
	if groupName == "" {
		return nil, nil
	}
	var output *awsautoscaling.DescribeLifecycleHooksOutput
	err := c.recordAPICall(ctx, "DescribeLifecycleHooks", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLifecycleHooks(callCtx, &awsautoscaling.DescribeLifecycleHooksInput{
			AutoScalingGroupName: aws.String(groupName),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	hooks := make([]autoscalingservice.LifecycleHook, 0, len(output.LifecycleHooks))
	for _, detail := range output.LifecycleHooks {
		hooks = append(hooks, mapLifecycleHook(detail))
	}
	return hooks, nil
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

var _ autoscalingservice.Client = (*Client)(nil)

var _ apiClient = (*awsautoscaling.Client)(nil)
