// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the SageMaker SDK surface the adapter consumes. It is restricted
// to List and Describe reads plus ListTags. It deliberately omits every
// mutation and every inference call. The inference operations (InvokeEndpoint /
// InvokeEndpointAsync) are not part of the aws-sdk-go-v2/service/sagemaker
// control-plane client at all; they live in the separate sagemakerruntime
// module this package never imports. exclusion_test.go enforces the contract.
type apiClient interface {
	ListNotebookInstances(context.Context, *awssagemaker.ListNotebookInstancesInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListNotebookInstancesOutput, error)
	DescribeNotebookInstance(context.Context, *awssagemaker.DescribeNotebookInstanceInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeNotebookInstanceOutput, error)
	ListModels(context.Context, *awssagemaker.ListModelsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListModelsOutput, error)
	DescribeModel(context.Context, *awssagemaker.DescribeModelInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeModelOutput, error)
	ListEndpoints(context.Context, *awssagemaker.ListEndpointsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListEndpointsOutput, error)
	DescribeEndpoint(context.Context, *awssagemaker.DescribeEndpointInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeEndpointOutput, error)
	ListEndpointConfigs(context.Context, *awssagemaker.ListEndpointConfigsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListEndpointConfigsOutput, error)
	DescribeEndpointConfig(context.Context, *awssagemaker.DescribeEndpointConfigInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeEndpointConfigOutput, error)
	ListTrainingJobs(context.Context, *awssagemaker.ListTrainingJobsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListTrainingJobsOutput, error)
	DescribeTrainingJob(context.Context, *awssagemaker.DescribeTrainingJobInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeTrainingJobOutput, error)
	ListProcessingJobs(context.Context, *awssagemaker.ListProcessingJobsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListProcessingJobsOutput, error)
	ListTransformJobs(context.Context, *awssagemaker.ListTransformJobsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListTransformJobsOutput, error)
	ListHyperParameterTuningJobs(context.Context, *awssagemaker.ListHyperParameterTuningJobsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListHyperParameterTuningJobsOutput, error)
	ListProjects(context.Context, *awssagemaker.ListProjectsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListProjectsOutput, error)
	ListPipelines(context.Context, *awssagemaker.ListPipelinesInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListPipelinesOutput, error)
	ListFeatureGroups(context.Context, *awssagemaker.ListFeatureGroupsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListFeatureGroupsOutput, error)
	ListDomains(context.Context, *awssagemaker.ListDomainsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListDomainsOutput, error)
	DescribeDomain(context.Context, *awssagemaker.DescribeDomainInput, ...func(*awssagemaker.Options)) (*awssagemaker.DescribeDomainOutput, error)
	ListUserProfiles(context.Context, *awssagemaker.ListUserProfilesInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListUserProfilesOutput, error)
	ListApps(context.Context, *awssagemaker.ListAppsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListAppsOutput, error)
	ListInferenceComponents(context.Context, *awssagemaker.ListInferenceComponentsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListInferenceComponentsOutput, error)
	ListTags(context.Context, *awssagemaker.ListTagsInput, ...func(*awssagemaker.Options)) (*awssagemaker.ListTagsOutput, error)
}

// Client adapts AWS SDK SageMaker control-plane reads into scanner-owned
// metadata. It never invokes endpoints, never mutates SageMaker state, and
// never reads protected payloads such as hyperparameter values, data
// references, lifecycle-config script bodies, or pipeline definition bodies.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a SageMaker SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssagemaker.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// listTags returns the bounded tag set for one SageMaker resource ARN. It
// returns nil for blank ARNs and Studio resources that lack an ARN in their
// list summary so the scanner never blocks on a tag read it cannot make.
func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	arn := strings.TrimSpace(resourceARN)
	if arn == "" {
		return nil, nil
	}
	tags := map[string]string{}
	paginator := awssagemaker.NewListTagsPaginator(c.client, &awssagemaker.ListTagsInput{
		ResourceArn: aws.String(arn),
	})
	for paginator.HasMorePages() {
		var page *awssagemaker.ListTagsOutput
		err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, tag := range page.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

// recordAPICall wraps one AWS API call with a pagination span and the shared
// AWS API-call and throttle counters so operators can diagnose SageMaker scans
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

var _ apiClient = (*awssagemaker.Client)(nil)
