// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsevents "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	awseventstypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	eventbridgeservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/eventbridge"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	ListEventBuses(context.Context, *awsevents.ListEventBusesInput, ...func(*awsevents.Options)) (*awsevents.ListEventBusesOutput, error)
	ListRules(context.Context, *awsevents.ListRulesInput, ...func(*awsevents.Options)) (*awsevents.ListRulesOutput, error)
	DescribeRule(context.Context, *awsevents.DescribeRuleInput, ...func(*awsevents.Options)) (*awsevents.DescribeRuleOutput, error)
	ListTargetsByRule(context.Context, *awsevents.ListTargetsByRuleInput, ...func(*awsevents.Options)) (*awsevents.ListTargetsByRuleOutput, error)
	ListTagsForResource(context.Context, *awsevents.ListTagsForResourceInput, ...func(*awsevents.Options)) (*awsevents.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK EventBridge pagination into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an EventBridge SDK adapter for one claimed AWS boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsevents.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListEventBuses returns EventBridge bus metadata visible to the configured AWS
// credentials. It reads rule, target, and tag metadata; it never calls PutEvents
// or persists target input payload fields.
func (c *Client) ListEventBuses(ctx context.Context) ([]eventbridgeservice.EventBus, error) {
	var buses []eventbridgeservice.EventBus
	var nextToken *string
	for {
		var page *awsevents.ListEventBusesOutput
		err := c.recordAPICall(ctx, "ListEventBuses", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEventBuses(callCtx, &awsevents.ListEventBusesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return buses, nil
		}
		for _, bus := range page.EventBuses {
			mapped, err := c.busMetadata(ctx, bus)
			if err != nil {
				return nil, err
			}
			buses = append(buses, mapped)
		}
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return buses, nil
		}
	}
}

func (c *Client) busMetadata(
	ctx context.Context,
	bus awseventstypes.EventBus,
) (eventbridgeservice.EventBus, error) {
	busARN := awsv2.ToString(bus.Arn)
	tags, err := c.listTags(ctx, busARN)
	if err != nil {
		return eventbridgeservice.EventBus{}, err
	}
	busName := awsv2.ToString(bus.Name)
	rules, err := c.listRules(ctx, busName)
	if err != nil {
		return eventbridgeservice.EventBus{}, err
	}
	return eventbridgeservice.EventBus{
		ARN:              strings.TrimSpace(busARN),
		Name:             strings.TrimSpace(busName),
		Description:      strings.TrimSpace(awsv2.ToString(bus.Description)),
		CreationTime:     awsv2.ToTime(bus.CreationTime),
		LastModifiedTime: awsv2.ToTime(bus.LastModifiedTime),
		Tags:             cloneStringMap(tags),
		Rules:            rules,
	}, nil
}

func (c *Client) listRules(ctx context.Context, eventBusName string) ([]eventbridgeservice.Rule, error) {
	eventBusName = strings.TrimSpace(eventBusName)
	if eventBusName == "" {
		return nil, nil
	}
	var rules []eventbridgeservice.Rule
	var nextToken *string
	for {
		var page *awsevents.ListRulesOutput
		err := c.recordAPICall(ctx, "ListRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRules(callCtx, &awsevents.ListRulesInput{
				EventBusName: awsv2.String(eventBusName),
				NextToken:    nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, rule := range page.Rules {
			mapped, err := c.ruleMetadata(ctx, eventBusName, rule)
			if err != nil {
				return nil, err
			}
			rules = append(rules, mapped)
		}
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return rules, nil
		}
	}
}

func (c *Client) ruleMetadata(
	ctx context.Context,
	eventBusName string,
	rule awseventstypes.Rule,
) (eventbridgeservice.Rule, error) {
	ruleName := awsv2.ToString(rule.Name)
	description, err := c.describeRule(ctx, eventBusName, ruleName)
	if err != nil {
		return eventbridgeservice.Rule{}, err
	}
	ruleARN := firstNonEmpty(awsv2.ToString(description.Arn), awsv2.ToString(rule.Arn))
	tags, err := c.listTags(ctx, ruleARN)
	if err != nil {
		return eventbridgeservice.Rule{}, err
	}
	targets, err := c.listTargets(ctx, eventBusName, ruleName)
	if err != nil {
		return eventbridgeservice.Rule{}, err
	}
	return eventbridgeservice.Rule{
		ARN:                strings.TrimSpace(ruleARN),
		Name:               strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.Name), ruleName)),
		EventBusName:       strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.EventBusName), awsv2.ToString(rule.EventBusName), eventBusName)),
		Description:        strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.Description), awsv2.ToString(rule.Description))),
		EventPattern:       strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.EventPattern), awsv2.ToString(rule.EventPattern))),
		ManagedBy:          strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.ManagedBy), awsv2.ToString(rule.ManagedBy))),
		RoleARN:            strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.RoleArn), awsv2.ToString(rule.RoleArn))),
		ScheduleExpression: strings.TrimSpace(firstNonEmpty(awsv2.ToString(description.ScheduleExpression), awsv2.ToString(rule.ScheduleExpression))),
		State:              strings.TrimSpace(firstNonEmpty(string(description.State), string(rule.State))),
		CreatedBy:          strings.TrimSpace(awsv2.ToString(description.CreatedBy)),
		Tags:               cloneStringMap(tags),
		Targets:            targets,
	}, nil
}

func (c *Client) describeRule(
	ctx context.Context,
	eventBusName string,
	ruleName string,
) (*awsevents.DescribeRuleOutput, error) {
	if strings.TrimSpace(ruleName) == "" {
		return &awsevents.DescribeRuleOutput{}, nil
	}
	var output *awsevents.DescribeRuleOutput
	err := c.recordAPICall(ctx, "DescribeRule", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeRule(callCtx, &awsevents.DescribeRuleInput{
			EventBusName: awsv2.String(eventBusName),
			Name:         awsv2.String(ruleName),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsevents.DescribeRuleOutput{}, nil
	}
	return output, nil
}

func (c *Client) listTargets(
	ctx context.Context,
	eventBusName string,
	ruleName string,
) ([]eventbridgeservice.Target, error) {
	ruleName = strings.TrimSpace(ruleName)
	if ruleName == "" {
		return nil, nil
	}
	var targets []eventbridgeservice.Target
	var nextToken *string
	for {
		var page *awsevents.ListTargetsByRuleOutput
		err := c.recordAPICall(ctx, "ListTargetsByRule", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTargetsByRule(callCtx, &awsevents.ListTargetsByRuleInput{
				EventBusName: awsv2.String(eventBusName),
				NextToken:    nextToken,
				Rule:         awsv2.String(ruleName),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return targets, nil
		}
		for _, target := range page.Targets {
			targets = append(targets, mapTarget(target))
		}
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return targets, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsevents.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsevents.ListTagsForResourceInput{
			ResourceARN: awsv2.String(resourceARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return tagsMap(output.Tags), nil
}

func mapTarget(target awseventstypes.Target) eventbridgeservice.Target {
	mapped := eventbridgeservice.Target{
		ID:      strings.TrimSpace(awsv2.ToString(target.Id)),
		ARN:     strings.TrimSpace(awsv2.ToString(target.Arn)),
		RoleARN: strings.TrimSpace(awsv2.ToString(target.RoleArn)),
	}
	if target.DeadLetterConfig != nil {
		mapped.DeadLetterARN = strings.TrimSpace(awsv2.ToString(target.DeadLetterConfig.Arn))
	}
	if target.RetryPolicy != nil {
		mapped.MaximumEventAgeInSeconds = awsv2.ToInt32(target.RetryPolicy.MaximumEventAgeInSeconds)
		mapped.MaximumRetryAttempts = awsv2.ToInt32(target.RetryPolicy.MaximumRetryAttempts)
	}
	return mapped
}

func tagsMap(tags []awseventstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(awsv2.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = awsv2.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
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
	aws.RecordAPICall(ctx, aws.APICallEvent{
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

var _ eventbridgeservice.Client = (*Client)(nil)

var _ apiClient = (*awsevents.Client)(nil)
