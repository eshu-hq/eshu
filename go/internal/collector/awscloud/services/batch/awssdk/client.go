// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbatch "github.com/aws/aws-sdk-go-v2/service/batch"
	awsbatchtypes "github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	batchservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// describeJobDefinitionsLimit bounds the DescribeJobDefinitions page size.
	describeJobDefinitionsLimit = 100
	// recentJobsPerStatus bounds the recent jobs listed per queue per active
	// status so the scan stays within the per-scope performance contract.
	recentJobsPerStatus = 100
)

// recentJobStatuses are the active Batch job states the scanner lists per queue
// for recent-jobs evidence. Terminal SUCCEEDED/FAILED history is intentionally
// excluded to keep the scan bounded and current.
var recentJobStatuses = []awsbatchtypes.JobStatus{
	awsbatchtypes.JobStatusSubmitted,
	awsbatchtypes.JobStatusPending,
	awsbatchtypes.JobStatusRunnable,
	awsbatchtypes.JobStatusStarting,
	awsbatchtypes.JobStatusRunning,
}

// apiClient is the metadata-only AWS Batch read surface used by the adapter.
// Only List and Describe reads appear here; no Submit/Cancel/Terminate,
// Register/Deregister, or Create/Update/Delete operation is reachable.
type apiClient interface {
	DescribeComputeEnvironments(context.Context, *awsbatch.DescribeComputeEnvironmentsInput, ...func(*awsbatch.Options)) (*awsbatch.DescribeComputeEnvironmentsOutput, error)
	DescribeJobQueues(context.Context, *awsbatch.DescribeJobQueuesInput, ...func(*awsbatch.Options)) (*awsbatch.DescribeJobQueuesOutput, error)
	DescribeJobDefinitions(context.Context, *awsbatch.DescribeJobDefinitionsInput, ...func(*awsbatch.Options)) (*awsbatch.DescribeJobDefinitionsOutput, error)
	DescribeSchedulingPolicies(context.Context, *awsbatch.DescribeSchedulingPoliciesInput, ...func(*awsbatch.Options)) (*awsbatch.DescribeSchedulingPoliciesOutput, error)
	ListSchedulingPolicies(context.Context, *awsbatch.ListSchedulingPoliciesInput, ...func(*awsbatch.Options)) (*awsbatch.ListSchedulingPoliciesOutput, error)
	ListJobs(context.Context, *awsbatch.ListJobsInput, ...func(*awsbatch.Options)) (*awsbatch.ListJobsOutput, error)
}

// Client adapts the AWS SDK for Go v2 Batch client into scanner-owned records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Batch SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsbatch.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListComputeEnvironments returns all Batch compute environments visible to the
// configured AWS credentials.
func (c *Client) ListComputeEnvironments(ctx context.Context) ([]batchservice.ComputeEnvironment, error) {
	paginator := awsbatch.NewDescribeComputeEnvironmentsPaginator(c.client, &awsbatch.DescribeComputeEnvironmentsInput{})
	var computeEnvironments []batchservice.ComputeEnvironment
	for paginator.HasMorePages() {
		var page *awsbatch.DescribeComputeEnvironmentsOutput
		err := c.recordAPICall(ctx, "DescribeComputeEnvironments", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.ComputeEnvironments {
			computeEnvironments = append(computeEnvironments, mapComputeEnvironment(detail))
		}
	}
	return computeEnvironments, nil
}

// ListJobQueues returns all Batch job queues visible to the configured AWS
// credentials.
func (c *Client) ListJobQueues(ctx context.Context) ([]batchservice.JobQueue, error) {
	paginator := awsbatch.NewDescribeJobQueuesPaginator(c.client, &awsbatch.DescribeJobQueuesInput{})
	var jobQueues []batchservice.JobQueue
	for paginator.HasMorePages() {
		var page *awsbatch.DescribeJobQueuesOutput
		err := c.recordAPICall(ctx, "DescribeJobQueues", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.JobQueues {
			jobQueues = append(jobQueues, mapJobQueue(detail))
		}
	}
	return jobQueues, nil
}

// ListJobDefinitions returns all ACTIVE Batch job definitions visible to the
// configured AWS credentials.
func (c *Client) ListJobDefinitions(ctx context.Context) ([]batchservice.JobDefinition, error) {
	paginator := awsbatch.NewDescribeJobDefinitionsPaginator(c.client, &awsbatch.DescribeJobDefinitionsInput{
		MaxResults: aws.Int32(describeJobDefinitionsLimit),
		Status:     aws.String("ACTIVE"),
	})
	var jobDefinitions []batchservice.JobDefinition
	for paginator.HasMorePages() {
		var page *awsbatch.DescribeJobDefinitionsOutput
		err := c.recordAPICall(ctx, "DescribeJobDefinitions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range page.JobDefinitions {
			jobDefinitions = append(jobDefinitions, mapJobDefinition(detail))
		}
	}
	return jobDefinitions, nil
}

// ListSchedulingPolicies returns all Batch scheduling policies visible to the
// configured AWS credentials. Only identity is read; DescribeSchedulingPolicies
// returns fair-share state, which the adapter discards before the scanner sees
// it.
func (c *Client) ListSchedulingPolicies(ctx context.Context) ([]batchservice.SchedulingPolicy, error) {
	paginator := awsbatch.NewListSchedulingPoliciesPaginator(c.client, &awsbatch.ListSchedulingPoliciesInput{})
	var arns []string
	for paginator.HasMorePages() {
		var page *awsbatch.ListSchedulingPoliciesOutput
		err := c.recordAPICall(ctx, "ListSchedulingPolicies", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, listing := range page.SchedulingPolicies {
			if arn := strings.TrimSpace(aws.ToString(listing.Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
	}
	var schedulingPolicies []batchservice.SchedulingPolicy
	for _, chunk := range chunkStrings(arns, describeJobDefinitionsLimit) {
		var output *awsbatch.DescribeSchedulingPoliciesOutput
		err := c.recordAPICall(ctx, "DescribeSchedulingPolicies", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeSchedulingPolicies(callCtx, &awsbatch.DescribeSchedulingPoliciesInput{
				Arns: chunk,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, detail := range output.SchedulingPolicies {
			schedulingPolicies = append(schedulingPolicies, mapSchedulingPolicy(detail))
		}
	}
	return schedulingPolicies, nil
}

// ListRecentJobs returns recent jobs for one queue across the active Batch job
// states, bounded per status by recentJobsPerStatus.
func (c *Client) ListRecentJobs(ctx context.Context, queue batchservice.JobQueue) ([]batchservice.Job, error) {
	queueARN := strings.TrimSpace(firstNonEmpty(queue.ARN, queue.Name))
	if queueARN == "" {
		return nil, nil
	}
	var jobs []batchservice.Job
	seen := make(map[string]struct{})
	for _, status := range recentJobStatuses {
		paginator := awsbatch.NewListJobsPaginator(c.client, &awsbatch.ListJobsInput{
			JobQueue:   aws.String(queueARN),
			JobStatus:  status,
			MaxResults: aws.Int32(recentJobsPerStatus),
		})
		remaining := recentJobsPerStatus
		for paginator.HasMorePages() && remaining > 0 {
			var page *awsbatch.ListJobsOutput
			err := c.recordAPICall(ctx, "ListJobs", func(callCtx context.Context) error {
				var err error
				page, err = paginator.NextPage(callCtx)
				return err
			})
			if err != nil {
				return nil, err
			}
			for _, summary := range page.JobSummaryList {
				jobID := strings.TrimSpace(aws.ToString(summary.JobId))
				if jobID == "" {
					continue
				}
				if _, ok := seen[jobID]; ok {
					continue
				}
				seen[jobID] = struct{}{}
				jobs = append(jobs, mapJob(summary, queueARN))
				remaining--
				if remaining <= 0 {
					break
				}
			}
		}
	}
	return jobs, nil
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

func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 || size <= 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var _ batchservice.Client = (*Client)(nil)

var _ apiClient = (*awsbatch.Client)(nil)
