// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfn "github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cfnservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudformation"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal CloudFormation SDK surface the adapter consumes. It
// deliberately excludes every template-body extraction API (GetTemplate,
// GetTemplateSummary), the change-set body API (DescribeChangeSet,
// DescribeChangeSetHooks), the stack-policy body API (GetStackPolicy), and the
// full mutation surface (Create/Update/Delete Stack, Create/Update/Delete
// StackSet, Create/Execute/Delete ChangeSet, RollbackStack,
// ContinueUpdateRollback, CancelUpdateStack, Create/Update/Delete
// StackInstances, Detect*Drift, Register/Deregister/Activate/Deactivate Type).
//
// Adding any forbidden method here weakens the redaction contract; the
// adapter-side guard test TestAPIClientInterfaceExcludesTemplateAndMutationAPIs
// and the scanner-side guard exist to catch such regressions.
type apiClient interface {
	DescribeStacks(context.Context, *awscfn.DescribeStacksInput, ...func(*awscfn.Options)) (*awscfn.DescribeStacksOutput, error)
	ListStacks(context.Context, *awscfn.ListStacksInput, ...func(*awscfn.Options)) (*awscfn.ListStacksOutput, error)
	ListStackResources(context.Context, *awscfn.ListStackResourcesInput, ...func(*awscfn.Options)) (*awscfn.ListStackResourcesOutput, error)
	ListStackSets(context.Context, *awscfn.ListStackSetsInput, ...func(*awscfn.Options)) (*awscfn.ListStackSetsOutput, error)
	DescribeStackSet(context.Context, *awscfn.DescribeStackSetInput, ...func(*awscfn.Options)) (*awscfn.DescribeStackSetOutput, error)
	ListChangeSets(context.Context, *awscfn.ListChangeSetsInput, ...func(*awscfn.Options)) (*awscfn.ListChangeSetsOutput, error)
	DescribeStackResourceDrifts(context.Context, *awscfn.DescribeStackResourceDriftsInput, ...func(*awscfn.Options)) (*awscfn.DescribeStackResourceDriftsOutput, error)
	ListStackInstances(context.Context, *awscfn.ListStackInstancesInput, ...func(*awscfn.Options)) (*awscfn.ListStackInstancesOutput, error)
	ListTypes(context.Context, *awscfn.ListTypesInput, ...func(*awscfn.Options)) (*awscfn.ListTypesOutput, error)
}

// Client adapts AWS SDK CloudFormation control-plane reads into metadata-only
// scanner records. It never reads a template body, parameter value, change-set
// body, stack policy, or drift property document.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudFormation SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscfn.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// deletedStackStatuses are the stack statuses the scanner treats as recently
// deleted. DescribeStacks does not return DELETE_COMPLETE stacks, so the
// adapter discovers them through a status-filtered ListStacks call.
var deletedStackStatuses = []cfntypes.StackStatus{cfntypes.StackStatusDeleteComplete}

// ListStacks returns active stack configuration (via DescribeStacks) plus
// recently deleted stack summaries (via a status-filtered ListStacks). It never
// calls GetTemplate, GetTemplateSummary, or any mutation API.
func (c *Client) ListStacks(ctx context.Context) ([]cfnservice.Stack, error) {
	stacks, err := c.describeActiveStacks(ctx)
	if err != nil {
		return nil, err
	}
	deleted, err := c.listDeletedStacks(ctx)
	if err != nil {
		return nil, err
	}
	return append(stacks, deleted...), nil
}

func (c *Client) describeActiveStacks(ctx context.Context) ([]cfnservice.Stack, error) {
	paginator := awscfn.NewDescribeStacksPaginator(c.client, &awscfn.DescribeStacksInput{})
	var stacks []cfnservice.Stack
	for paginator.HasMorePages() {
		var page *awscfn.DescribeStacksOutput
		err := c.recordAPICall(ctx, "DescribeStacks", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Stacks {
			stacks = append(stacks, mapStack(page.Stacks[i]))
		}
	}
	return stacks, nil
}

func (c *Client) listDeletedStacks(ctx context.Context) ([]cfnservice.Stack, error) {
	paginator := awscfn.NewListStacksPaginator(c.client, &awscfn.ListStacksInput{
		StackStatusFilter: deletedStackStatuses,
	})
	var stacks []cfnservice.Stack
	for paginator.HasMorePages() {
		var page *awscfn.ListStacksOutput
		err := c.recordAPICall(ctx, "ListStacks", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.StackSummaries {
			stacks = append(stacks, mapDeletedStack(page.StackSummaries[i]))
		}
	}
	return stacks, nil
}

// ListStackResources returns the resource-type summary for one stack. It
// carries the resource type, logical/physical identity, and status only; no
// resource property body is read.
func (c *Client) ListStackResources(ctx context.Context, stackID string) ([]cfnservice.StackResource, error) {
	paginator := awscfn.NewListStackResourcesPaginator(c.client, &awscfn.ListStackResourcesInput{
		StackName: aws.String(stackID),
	})
	var resources []cfnservice.StackResource
	for paginator.HasMorePages() {
		var page *awscfn.ListStackResourcesOutput
		err := c.recordAPICall(ctx, "ListStackResources", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.StackResourceSummaries {
			resources = append(resources, mapStackResource(page.StackResourceSummaries[i]))
		}
	}
	return resources, nil
}

// ListStackSets returns stack-set metadata. It paginates ListStackSets, then
// calls DescribeStackSet per stack set for capabilities, role references, and
// parameter keys. The stack-set TemplateBody and parameter values are dropped
// during mapping and never enter the scanner type.
func (c *Client) ListStackSets(ctx context.Context) ([]cfnservice.StackSet, error) {
	paginator := awscfn.NewListStackSetsPaginator(c.client, &awscfn.ListStackSetsInput{})
	var stackSets []cfnservice.StackSet
	for paginator.HasMorePages() {
		var page *awscfn.ListStackSetsOutput
		err := c.recordAPICall(ctx, "ListStackSets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Summaries {
			stackSet, err := c.describeStackSet(ctx, page.Summaries[i])
			if err != nil {
				return nil, err
			}
			stackSets = append(stackSets, stackSet)
		}
	}
	return stackSets, nil
}

func (c *Client) describeStackSet(ctx context.Context, summary cfntypes.StackSetSummary) (cfnservice.StackSet, error) {
	name := strings.TrimSpace(aws.ToString(summary.StackSetName))
	var output *awscfn.DescribeStackSetOutput
	err := c.recordAPICall(ctx, "DescribeStackSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeStackSet(callCtx, &awscfn.DescribeStackSetInput{
			StackSetName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return cfnservice.StackSet{}, err
	}
	return mapStackSet(summary, output), nil
}

// ListChangeSets returns change-set metadata for one stack. It only paginates
// ListChangeSets and never calls DescribeChangeSet, so the per-resource change
// body is never read.
func (c *Client) ListChangeSets(ctx context.Context, stackID string) ([]cfnservice.ChangeSet, error) {
	paginator := awscfn.NewListChangeSetsPaginator(c.client, &awscfn.ListChangeSetsInput{
		StackName: aws.String(stackID),
	})
	var changeSets []cfnservice.ChangeSet
	for paginator.HasMorePages() {
		var page *awscfn.ListChangeSetsOutput
		err := c.recordAPICall(ctx, "ListChangeSets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Summaries {
			changeSets = append(changeSets, mapChangeSet(page.Summaries[i]))
		}
	}
	return changeSets, nil
}

// ListStackResourceDrifts returns the most recent drift detection result for one
// stack as per-status counts. It reads DescribeStackResourceDrifts (a read of
// the last detection result) and never calls DetectStackDrift, so it never
// triggers a new detection run. Actual and expected property documents are
// discarded during mapping.
func (c *Client) ListStackResourceDrifts(ctx context.Context, stackID string) (cfnservice.StackDriftResult, error) {
	paginator := awscfn.NewDescribeStackResourceDriftsPaginator(c.client, &awscfn.DescribeStackResourceDriftsInput{
		StackName: aws.String(stackID),
	})
	result := cfnservice.StackDriftResult{StackID: strings.TrimSpace(stackID)}
	for paginator.HasMorePages() {
		var page *awscfn.DescribeStackResourceDriftsOutput
		err := c.recordAPICall(ctx, "DescribeStackResourceDrifts", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return cfnservice.StackDriftResult{}, err
		}
		for i := range page.StackResourceDrifts {
			accumulateDrift(&result, page.StackResourceDrifts[i].StackResourceDriftStatus)
		}
	}
	return result, nil
}

// ListStackInstances returns stack-instance metadata for one stack set.
func (c *Client) ListStackInstances(ctx context.Context, stackSetName string) ([]cfnservice.StackInstance, error) {
	paginator := awscfn.NewListStackInstancesPaginator(c.client, &awscfn.ListStackInstancesInput{
		StackSetName: aws.String(stackSetName),
	})
	var instances []cfnservice.StackInstance
	for paginator.HasMorePages() {
		var page *awscfn.ListStackInstancesOutput
		err := c.recordAPICall(ctx, "ListStackInstances", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Summaries {
			instances = append(instances, mapStackInstance(page.Summaries[i]))
		}
	}
	return instances, nil
}

// ListTypes returns activated registered extension (type) metadata.
func (c *Client) ListTypes(ctx context.Context) ([]cfnservice.RegisteredType, error) {
	paginator := awscfn.NewListTypesPaginator(c.client, &awscfn.ListTypesInput{
		Visibility: cfntypes.VisibilityPrivate,
	})
	var types []cfnservice.RegisteredType
	for paginator.HasMorePages() {
		var page *awscfn.ListTypesOutput
		err := c.recordAPICall(ctx, "ListTypes", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for i := range page.TypeSummaries {
			types = append(types, mapRegisteredType(page.TypeSummaries[i]))
		}
	}
	return types, nil
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

var _ cfnservice.Client = (*Client)(nil)

var _ apiClient = (*awscfn.Client)(nil)
