// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssfn "github.com/aws/aws-sdk-go-v2/service/sfn"
	awssfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	stepfunctionsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/stepfunctions"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	ListStateMachines(context.Context, *awssfn.ListStateMachinesInput, ...func(*awssfn.Options)) (*awssfn.ListStateMachinesOutput, error)
	DescribeStateMachine(context.Context, *awssfn.DescribeStateMachineInput, ...func(*awssfn.Options)) (*awssfn.DescribeStateMachineOutput, error)
	ListActivities(context.Context, *awssfn.ListActivitiesInput, ...func(*awssfn.Options)) (*awssfn.ListActivitiesOutput, error)
	ListTagsForResource(context.Context, *awssfn.ListTagsForResourceInput, ...func(*awssfn.Options)) (*awssfn.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Step Functions pagination into scanner-owned
// metadata. The adapter is the only place that talks to the AWS SDK; it
// projects the raw responses into the metadata-only contract that the
// scanner persists.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Step Functions SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssfn.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListStateMachines returns Step Functions state machine metadata visible to
// the configured AWS credentials. It reads list, describe, and tag metadata;
// it never calls StartExecution, CreateStateMachine, UpdateStateMachine,
// DeleteStateMachine, SendTaskSuccess, SendTaskFailure, or any other mutation
// or execution-payload API.
func (c *Client) ListStateMachines(ctx context.Context) ([]stepfunctionsservice.StateMachine, error) {
	var stateMachines []stepfunctionsservice.StateMachine
	var nextToken *string
	for {
		var page *awssfn.ListStateMachinesOutput
		err := c.recordAPICall(ctx, "ListStateMachines", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListStateMachines(callCtx, &awssfn.ListStateMachinesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return stateMachines, nil
		}
		for _, item := range page.StateMachines {
			machine, err := c.stateMachineMetadata(ctx, item)
			if err != nil {
				return nil, err
			}
			stateMachines = append(stateMachines, machine)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return stateMachines, nil
		}
	}
}

// ListActivities returns Step Functions activity metadata visible to the
// configured AWS credentials. It never calls GetActivityTask, SendTaskSuccess,
// SendTaskFailure, or any other task-payload API.
func (c *Client) ListActivities(ctx context.Context) ([]stepfunctionsservice.Activity, error) {
	var activities []stepfunctionsservice.Activity
	var nextToken *string
	for {
		var page *awssfn.ListActivitiesOutput
		err := c.recordAPICall(ctx, "ListActivities", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListActivities(callCtx, &awssfn.ListActivitiesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return activities, nil
		}
		for _, item := range page.Activities {
			activity, err := c.activityMetadata(ctx, item)
			if err != nil {
				return nil, err
			}
			activities = append(activities, activity)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return activities, nil
		}
	}
}

func (c *Client) stateMachineMetadata(
	ctx context.Context,
	item awssfntypes.StateMachineListItem,
) (stepfunctionsservice.StateMachine, error) {
	machineARN := aws.ToString(item.StateMachineArn)
	described, err := c.describeStateMachine(ctx, machineARN)
	if err != nil {
		return stepfunctionsservice.StateMachine{}, err
	}
	tags, err := c.listTags(ctx, machineARN)
	if err != nil {
		return stepfunctionsservice.StateMachine{}, err
	}
	startAt, states, refs := parseDefinition(aws.ToString(described.Definition))
	tracing := false
	if described.TracingConfiguration != nil {
		tracing = described.TracingConfiguration.Enabled
	}
	loggingLevel := ""
	if described.LoggingConfiguration != nil {
		loggingLevel = string(described.LoggingConfiguration.Level)
	}
	creation := aws.ToTime(described.CreationDate)
	if creation.IsZero() {
		creation = aws.ToTime(item.CreationDate)
	}
	return stepfunctionsservice.StateMachine{
		ARN:            strings.TrimSpace(machineARN),
		Name:           strings.TrimSpace(firstNonEmpty(aws.ToString(described.Name), aws.ToString(item.Name))),
		Type:           strings.TrimSpace(string(firstStateMachineType(described.Type, item.Type))),
		Status:         strings.TrimSpace(string(described.Status)),
		RoleARN:        strings.TrimSpace(aws.ToString(described.RoleArn)),
		CreationDate:   creation,
		LoggingLevel:   strings.TrimSpace(loggingLevel),
		TracingEnabled: tracing,
		StartAt:        startAt,
		States:         states,
		ReferencedARNs: refs,
		Tags:           cloneStringMap(tags),
	}, nil
}

func (c *Client) activityMetadata(
	ctx context.Context,
	item awssfntypes.ActivityListItem,
) (stepfunctionsservice.Activity, error) {
	activityARN := aws.ToString(item.ActivityArn)
	tags, err := c.listTags(ctx, activityARN)
	if err != nil {
		return stepfunctionsservice.Activity{}, err
	}
	return stepfunctionsservice.Activity{
		ARN:          strings.TrimSpace(activityARN),
		Name:         strings.TrimSpace(aws.ToString(item.Name)),
		CreationDate: aws.ToTime(item.CreationDate),
		Tags:         cloneStringMap(tags),
	}, nil
}

func (c *Client) describeStateMachine(
	ctx context.Context,
	stateMachineARN string,
) (*awssfn.DescribeStateMachineOutput, error) {
	if strings.TrimSpace(stateMachineARN) == "" {
		return &awssfn.DescribeStateMachineOutput{}, nil
	}
	var output *awssfn.DescribeStateMachineOutput
	err := c.recordAPICall(ctx, "DescribeStateMachine", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeStateMachine(callCtx, &awssfn.DescribeStateMachineInput{
			StateMachineArn: aws.String(stateMachineARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awssfn.DescribeStateMachineOutput{}, nil
	}
	return output, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awssfn.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awssfn.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
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

// parseDefinition projects the safe structural shape of a state machine
// definition. It extracts the StartAt entry point, per-state type and
// transition edges, and Task Resource ARN references. It never returns
// Parameters, ResultPath, ResultSelector, InputPath, OutputPath, Result,
// Cause, or Error literal contents from the source document, and it never
// returns the raw definition string.
//
// Malformed definitions are tolerated: parsing returns whatever shape could
// be extracted so a single bad definition cannot fail the entire scan. The
// caller is responsible for treating an empty StartAt or empty State list as
// "structure not available."
func parseDefinition(definition string) (string, []stepfunctionsservice.StateNode, []string) {
	trimmed := strings.TrimSpace(definition)
	if trimmed == "" {
		return "", nil, nil
	}
	var doc struct {
		StartAt string                     `json:"StartAt"`
		States  map[string]json.RawMessage `json:"States"`
	}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return "", nil, nil
	}
	if len(doc.States) == 0 {
		return strings.TrimSpace(doc.StartAt), nil, nil
	}
	names := make([]string, 0, len(doc.States))
	for name := range doc.States {
		names = append(names, name)
	}
	sort.Strings(names)
	states := make([]stepfunctionsservice.StateNode, 0, len(names))
	references := make(map[string]struct{}, len(names))
	for _, name := range names {
		raw := doc.States[name]
		var state struct {
			Type     string          `json:"Type"`
			Next     string          `json:"Next"`
			End      bool            `json:"End"`
			Default  string          `json:"Default"`
			Resource string          `json:"Resource"`
			Choices  json.RawMessage `json:"Choices"`
			Catch    json.RawMessage `json:"Catch"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			continue
		}
		node := stepfunctionsservice.StateNode{
			Name:        strings.TrimSpace(name),
			Type:        strings.TrimSpace(state.Type),
			Next:        strings.TrimSpace(state.Next),
			End:         state.End,
			Default:     strings.TrimSpace(state.Default),
			ResourceARN: strings.TrimSpace(state.Resource),
		}
		node.Choices = collectChoiceTransitions(state.Choices)
		node.CatchNext = collectCatchTransitions(state.Catch)
		states = append(states, node)
		if strings.HasPrefix(node.ResourceARN, "arn:") {
			references[node.ResourceARN] = struct{}{}
		}
	}
	refs := make([]string, 0, len(references))
	for ref := range references {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return strings.TrimSpace(doc.StartAt), states, refs
}

// collectChoiceTransitions returns the list of Next state names reachable from
// the choices array. It never reads choice condition payload contents.
func collectChoiceTransitions(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var choices []struct {
		Next string `json:"Next"`
	}
	if err := json.Unmarshal(raw, &choices); err != nil {
		return nil
	}
	out := make([]string, 0, len(choices))
	for _, choice := range choices {
		if next := strings.TrimSpace(choice.Next); next != "" {
			out = append(out, next)
		}
	}
	return out
}

// collectCatchTransitions returns the list of Next state names reachable from
// the catch array. It never reads error or cause payload contents.
func collectCatchTransitions(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var catches []struct {
		Next string `json:"Next"`
	}
	if err := json.Unmarshal(raw, &catches); err != nil {
		return nil
	}
	out := make([]string, 0, len(catches))
	for _, catch := range catches {
		if next := strings.TrimSpace(catch.Next); next != "" {
			out = append(out, next)
		}
	}
	return out
}

func firstStateMachineType(values ...awssfntypes.StateMachineType) awssfntypes.StateMachineType {
	for _, value := range values {
		if string(value) != "" {
			return value
		}
	}
	return ""
}

func tagsMap(tags []awssfntypes.Tag) map[string]string {
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

var _ stepfunctionsservice.Client = (*Client)(nil)

var _ apiClient = (*awssfn.Client)(nil)
