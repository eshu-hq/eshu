// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodepipeline "github.com/aws/aws-sdk-go-v2/service/codepipeline"
	cptypes "github.com/aws/aws-sdk-go-v2/service/codepipeline/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cpservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codepipeline"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recentExecutionLimit bounds how many recent execution summaries the adapter
// resolves per pipeline so the scan stays metadata-sized regardless of pipeline
// history depth.
const recentExecutionLimit = 25

// apiClient is the metadata-only CodePipeline SDK surface the adapter consumes.
// It intentionally omits every mutation API, execution-control API, webhook
// management API, custom-action mutation API, and job-worker API. The
// reflection guard test asserts the omission, and the job-worker plane is
// excluded because it returns action configuration secret values.
type apiClient interface {
	ListPipelines(context.Context, *awscodepipeline.ListPipelinesInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.ListPipelinesOutput, error)
	GetPipeline(context.Context, *awscodepipeline.GetPipelineInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.GetPipelineOutput, error)
	ListPipelineExecutions(context.Context, *awscodepipeline.ListPipelineExecutionsInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.ListPipelineExecutionsOutput, error)
	ListWebhooks(context.Context, *awscodepipeline.ListWebhooksInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.ListWebhooksOutput, error)
	ListActionTypes(context.Context, *awscodepipeline.ListActionTypesInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.ListActionTypesOutput, error)
	ListTagsForResource(context.Context, *awscodepipeline.ListTagsForResourceInput, ...func(*awscodepipeline.Options)) (*awscodepipeline.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CodePipeline pagination into scanner-owned metadata. It
// drops every action configuration value and the webhook authentication secret
// token, and redacts source-revision summaries before they reach scanner types.
type Client struct {
	client       apiClient
	boundary     awscloud.Boundary
	tracer       trace.Tracer
	instruments  *telemetry.Instruments
	redactionKey redact.Key
}

// NewClient builds a CodePipeline SDK adapter for one claimed AWS boundary. The
// redaction key is required so source-revision summaries never persist raw;
// callers obtain it from the runtime scanner dependencies.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	redactionKey redact.Key,
) *Client {
	return &Client{
		client:       awscodepipeline.NewFromConfig(config),
		boundary:     boundary,
		tracer:       tracer,
		instruments:  instruments,
		redactionKey: redactionKey,
	}
}

// ListPipelines returns pipeline metadata visible to the configured
// credentials. It paginates ListPipelines and resolves each pipeline's full
// declaration through GetPipeline and its tags through ListTagsForResource.
func (c *Client) ListPipelines(ctx context.Context) ([]cpservice.Pipeline, error) {
	names, err := c.listPipelineNames(ctx)
	if err != nil {
		return nil, err
	}
	pipelines := make([]cpservice.Pipeline, 0, len(names))
	for _, name := range names {
		var output *awscodepipeline.GetPipelineOutput
		err := c.recordAPICall(ctx, "GetPipeline", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.GetPipeline(callCtx, &awscodepipeline.GetPipelineInput{
				Name: aws.String(name),
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil || output.Pipeline == nil {
			continue
		}
		pipeline := mapPipelineDeclaration(output.Pipeline, output.Metadata)
		tags, err := c.listTags(ctx, pipeline.ARN)
		if err != nil {
			return nil, err
		}
		pipeline.Tags = tags
		pipelines = append(pipelines, pipeline)
	}
	return pipelines, nil
}

func (c *Client) listPipelineNames(ctx context.Context) ([]string, error) {
	var names []string
	var token *string
	for {
		var output *awscodepipeline.ListPipelinesOutput
		err := c.recordAPICall(ctx, "ListPipelines", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListPipelines(callCtx, &awscodepipeline.ListPipelinesInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, summary := range output.Pipelines {
			if name := strings.TrimSpace(aws.ToString(summary.Name)); name != "" {
				names = append(names, name)
			}
		}
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return names, nil
}

// ListRecentExecutions returns the most recent execution summaries for one
// pipeline, bounded by recentExecutionLimit so the scan stays metadata-sized.
func (c *Client) ListRecentExecutions(ctx context.Context, pipelineName string) ([]cpservice.Execution, error) {
	pipelineName = strings.TrimSpace(pipelineName)
	if pipelineName == "" {
		return nil, nil
	}
	var output *awscodepipeline.ListPipelineExecutionsOutput
	err := c.recordAPICall(ctx, "ListPipelineExecutions", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListPipelineExecutions(callCtx, &awscodepipeline.ListPipelineExecutionsInput{
			PipelineName: aws.String(pipelineName),
			MaxResults:   aws.Int32(recentExecutionLimit),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	summaries := output.PipelineExecutionSummaries
	if len(summaries) > recentExecutionLimit {
		summaries = summaries[:recentExecutionLimit]
	}
	executions := make([]cpservice.Execution, 0, len(summaries))
	for _, summary := range summaries {
		executions = append(executions, mapExecution(pipelineName, summary, c.redactionKey))
	}
	return executions, nil
}

// ListWebhooks returns webhook metadata for the boundary. The authentication
// secret token is never read into the scanner type.
func (c *Client) ListWebhooks(ctx context.Context) ([]cpservice.Webhook, error) {
	var webhooks []cpservice.Webhook
	var token *string
	for {
		var output *awscodepipeline.ListWebhooksOutput
		err := c.recordAPICall(ctx, "ListWebhooks", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListWebhooks(callCtx, &awscodepipeline.ListWebhooksInput{NextToken: token})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, item := range output.Webhooks {
			webhooks = append(webhooks, mapWebhook(item))
		}
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return webhooks, nil
}

// ListCustomActionTypes returns customer-defined action-type metadata for the
// boundary. AWS-owned and ThirdParty action types are filtered out so the scan
// stays scoped to custom action types.
func (c *Client) ListCustomActionTypes(ctx context.Context) ([]cpservice.ActionType, error) {
	var actionTypes []cpservice.ActionType
	var token *string
	for {
		var output *awscodepipeline.ListActionTypesOutput
		err := c.recordAPICall(ctx, "ListActionTypes", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListActionTypes(callCtx, &awscodepipeline.ListActionTypesInput{
				ActionOwnerFilter: cptypes.ActionOwnerCustom,
				NextToken:         token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, actionType := range output.ActionTypes {
			if actionType.Id == nil || actionType.Id.Owner != cptypes.ActionOwnerCustom {
				continue
			}
			actionTypes = append(actionTypes, mapActionType(actionType))
		}
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return actionTypes, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var token *string
	for {
		var output *awscodepipeline.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListTagsForResource(callCtx, &awscodepipeline.ListTagsForResourceInput{
				ResourceArn: aws.String(resourceARN),
				NextToken:   token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func pipelineMetadataARN(metadata *cptypes.PipelineMetadata) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(metadata.PipelineArn))
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

var _ apiClient = (*awscodepipeline.Client)(nil)

var _ cpservice.Client = (*Client)(nil)
