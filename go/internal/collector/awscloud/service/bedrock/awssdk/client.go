// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awsbedrockagent "github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// bedrockAPIClient is the Bedrock control-plane SDK surface the adapter
// consumes. It is restricted to List, Get, and ListTagsForResource reads. It
// deliberately omits every mutation (Create/Delete/Update/Stop/Start of any
// resource) and every inference call. The inference operations live in the
// separate aws-sdk-go-v2/service/bedrockruntime module (InvokeModel,
// InvokeModelWithResponseStream, Converse, ConverseStream), which this package
// never imports, so they cannot appear on this interface at all.
// exclusion_test.go enforces the contract.
type bedrockAPIClient interface {
	ListFoundationModels(context.Context, *awsbedrock.ListFoundationModelsInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListFoundationModelsOutput, error)
	ListCustomModels(context.Context, *awsbedrock.ListCustomModelsInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListCustomModelsOutput, error)
	GetCustomModel(context.Context, *awsbedrock.GetCustomModelInput, ...func(*awsbedrock.Options)) (*awsbedrock.GetCustomModelOutput, error)
	ListModelCustomizationJobs(context.Context, *awsbedrock.ListModelCustomizationJobsInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListModelCustomizationJobsOutput, error)
	ListProvisionedModelThroughputs(context.Context, *awsbedrock.ListProvisionedModelThroughputsInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListProvisionedModelThroughputsOutput, error)
	ListGuardrails(context.Context, *awsbedrock.ListGuardrailsInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListGuardrailsOutput, error)
	ListTagsForResource(context.Context, *awsbedrock.ListTagsForResourceInput, ...func(*awsbedrock.Options)) (*awsbedrock.ListTagsForResourceOutput, error)
}

// bedrockAgentAPIClient is the Bedrock Agents control-plane SDK surface the
// adapter consumes. It is restricted to List, Get, and ListTagsForResource
// reads. It deliberately omits every mutation (Create/Delete/Update/Prepare/
// StartIngestionJob) and every inference call. The agent inference operations
// live in the separate aws-sdk-go-v2/service/bedrockagentruntime module
// (InvokeAgent, Retrieve, RetrieveAndGenerate), which this package never
// imports, so they cannot appear on this interface at all. exclusion_test.go
// enforces the contract.
type bedrockAgentAPIClient interface {
	ListAgents(context.Context, *awsbedrockagent.ListAgentsInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentsOutput, error)
	GetAgent(context.Context, *awsbedrockagent.GetAgentInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetAgentOutput, error)
	ListAgentKnowledgeBases(context.Context, *awsbedrockagent.ListAgentKnowledgeBasesInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentKnowledgeBasesOutput, error)
	ListAgentActionGroups(context.Context, *awsbedrockagent.ListAgentActionGroupsInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentActionGroupsOutput, error)
	GetAgentActionGroup(context.Context, *awsbedrockagent.GetAgentActionGroupInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetAgentActionGroupOutput, error)
	ListKnowledgeBases(context.Context, *awsbedrockagent.ListKnowledgeBasesInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListKnowledgeBasesOutput, error)
	GetKnowledgeBase(context.Context, *awsbedrockagent.GetKnowledgeBaseInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetKnowledgeBaseOutput, error)
	ListDataSources(context.Context, *awsbedrockagent.ListDataSourcesInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListDataSourcesOutput, error)
	GetDataSource(context.Context, *awsbedrockagent.GetDataSourceInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetDataSourceOutput, error)
	ListTagsForResource(context.Context, *awsbedrockagent.ListTagsForResourceInput, ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Bedrock control-plane reads into scanner-owned
// metadata. It never invokes a model, never queries a knowledge base or agent,
// never mutates Bedrock state, and never reads protected payloads such as agent
// instructions, prompt-override configurations, guardrail policy bodies,
// knowledge base ingested document content, or action-group API schema bodies.
type Client struct {
	bedrock     bedrockAPIClient
	agent       bedrockAgentAPIClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Bedrock SDK adapter for one claimed AWS boundary. It
// constructs both control-plane SDK clients (bedrock and bedrock-agent); it
// never constructs a bedrock-runtime or bedrock-agent-runtime client.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		bedrock:     awsbedrock.NewFromConfig(config),
		agent:       awsbedrockagent.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// recordAPICall wraps one AWS API call with a pagination span and the shared
// AWS API-call and throttle counters so operators can diagnose Bedrock scans
// through the existing collector telemetry contract.
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

// page is the short alias the per-resource reads use to wrap one paginator page
// or point read in recordAPICall.
func (c *Client) page(ctx context.Context, operation string, call func(context.Context) error) error {
	return c.recordAPICall(ctx, operation, call)
}

// bedrockTags returns the bounded tag set for one Bedrock control-plane ARN. It
// returns nil for blank ARNs so the scanner never blocks on a tag read it
// cannot make.
func (c *Client) bedrockTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	arn := strings.TrimSpace(resourceARN)
	if arn == "" {
		return nil, nil
	}
	var output *awsbedrock.ListTagsForResourceOutput
	if err := c.page(ctx, "ListTagsForResource", func(callCtx context.Context) (err error) {
		output, err = c.bedrock.ListTagsForResource(callCtx, &awsbedrock.ListTagsForResourceInput{
			ResourceARN: aws.String(arn),
		})
		return err
	}); err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	tags := map[string]string{}
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

// agentTags returns the bounded tag set for one Bedrock Agents ARN. The
// bedrock-agent ListTagsForResource returns a map, unlike the bedrock
// control-plane key/value list, so the two tag readers stay separate.
func (c *Client) agentTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	arn := strings.TrimSpace(resourceARN)
	if arn == "" {
		return nil, nil
	}
	var output *awsbedrockagent.ListTagsForResourceOutput
	if err := c.page(ctx, "ListTagsForResource", func(callCtx context.Context) (err error) {
		output, err = c.agent.ListTagsForResource(callCtx, &awsbedrockagent.ListTagsForResourceInput{
			ResourceArn: aws.String(arn),
		})
		return err
	}); err != nil {
		return nil, err
	}
	if output == nil || len(output.Tags) == 0 {
		return nil, nil
	}
	tags := map[string]string{}
	for key, value := range output.Tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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

var (
	_ bedrockAPIClient      = (*awsbedrock.Client)(nil)
	_ bedrockAgentAPIClient = (*awsbedrockagent.Client)(nil)
)
