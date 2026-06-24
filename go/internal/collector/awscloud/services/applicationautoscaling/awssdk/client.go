// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaas "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	awsaastypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	aasservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/applicationautoscaling"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Application Auto Scaling API
// the adapter calls. It is deliberately limited to the three Describe reads. It
// exposes no Register/Deregister scalable target, no Put/Delete scaling policy
// or scheduled action, and no scaling-action operation, so the adapter cannot
// mutate scaling state. The exclusion_test reflects over this interface to
// enforce that contract at build time.
type apiClient interface {
	DescribeScalableTargets(
		context.Context,
		*awsaas.DescribeScalableTargetsInput,
		...func(*awsaas.Options),
	) (*awsaas.DescribeScalableTargetsOutput, error)
	DescribeScalingPolicies(
		context.Context,
		*awsaas.DescribeScalingPoliciesInput,
		...func(*awsaas.Options),
	) (*awsaas.DescribeScalingPoliciesOutput, error)
	DescribeScheduledActions(
		context.Context,
		*awsaas.DescribeScheduledActionsInput,
		...func(*awsaas.Options),
	) (*awsaas.DescribeScheduledActionsOutput, error)
}

// supportedNamespaces is the set of Application Auto Scaling service namespaces
// the scanner iterates. Each Describe call requires a namespace, so the adapter
// fans out across this set to enumerate every registered target, policy, and
// scheduled action regardless of the governed service.
var supportedNamespaces = []awsaastypes.ServiceNamespace{
	awsaastypes.ServiceNamespaceDynamodb,
	awsaastypes.ServiceNamespaceEcs,
	awsaastypes.ServiceNamespaceRds,
	awsaastypes.ServiceNamespaceEc2,
	awsaastypes.ServiceNamespaceLambda,
	awsaastypes.ServiceNamespaceAppstream,
	awsaastypes.ServiceNamespaceSagemaker,
	awsaastypes.ServiceNamespaceComprehend,
	awsaastypes.ServiceNamespaceCassandra,
	awsaastypes.ServiceNamespaceKafka,
	awsaastypes.ServiceNamespaceElasticache,
	awsaastypes.ServiceNamespaceNeptune,
	awsaastypes.ServiceNamespaceEmr,
	awsaastypes.ServiceNamespaceWorkspaces,
	awsaastypes.ServiceNamespaceCustomResource,
}

// Client adapts AWS SDK Application Auto Scaling control-plane calls into
// scanner-owned metadata. It never registers, deregisters, mutates, or invokes
// a scaling action.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Application Auto Scaling SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsaas.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Application Auto Scaling scalable targets, scaling policies,
// and scheduled actions visible to the configured AWS credentials, iterating
// every supported service namespace. A namespace that is throttled after SDK
// retries records a non-fatal warning and is skipped rather than failing the
// whole scan.
func (c *Client) Snapshot(ctx context.Context) (aasservice.Snapshot, error) {
	var snapshot aasservice.Snapshot
	for _, namespace := range supportedNamespaces {
		targets, warning, err := c.listScalableTargets(ctx, namespace)
		if err != nil {
			return aasservice.Snapshot{}, err
		}
		snapshot.ScalableTargets = append(snapshot.ScalableTargets, targets...)
		snapshot.Warnings = appendThrottleWarning(snapshot.Warnings, warning)

		policies, warning, err := c.listScalingPolicies(ctx, namespace)
		if err != nil {
			return aasservice.Snapshot{}, err
		}
		snapshot.ScalingPolicies = append(snapshot.ScalingPolicies, policies...)
		snapshot.Warnings = appendThrottleWarning(snapshot.Warnings, warning)

		actions, warning, err := c.listScheduledActions(ctx, namespace)
		if err != nil {
			return aasservice.Snapshot{}, err
		}
		snapshot.ScheduledActions = append(snapshot.ScheduledActions, actions...)
		snapshot.Warnings = appendThrottleWarning(snapshot.Warnings, warning)
	}
	return snapshot, nil
}

func (c *Client) listScalableTargets(
	ctx context.Context,
	namespace awsaastypes.ServiceNamespace,
) ([]aasservice.ScalableTarget, *awscloud.WarningObservation, error) {
	var targets []aasservice.ScalableTarget
	var nextToken *string
	for {
		var page *awsaas.DescribeScalableTargetsOutput
		err := c.recordAPICall(ctx, "DescribeScalableTargets", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeScalableTargets(callCtx, &awsaas.DescribeScalableTargetsInput{
				ServiceNamespace: namespace,
				NextToken:        nextToken,
			})
			return callErr
		})
		if err != nil {
			if isThrottleError(err) {
				return targets, c.throttleWarning(namespace, "DescribeScalableTargets", "scalable_targets"), nil
			}
			return nil, nil, err
		}
		if page == nil {
			return targets, nil, nil
		}
		for _, target := range page.ScalableTargets {
			targets = append(targets, mapScalableTarget(target))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return targets, nil, nil
		}
	}
}

func (c *Client) listScalingPolicies(
	ctx context.Context,
	namespace awsaastypes.ServiceNamespace,
) ([]aasservice.ScalingPolicy, *awscloud.WarningObservation, error) {
	var policies []aasservice.ScalingPolicy
	var nextToken *string
	for {
		var page *awsaas.DescribeScalingPoliciesOutput
		err := c.recordAPICall(ctx, "DescribeScalingPolicies", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeScalingPolicies(callCtx, &awsaas.DescribeScalingPoliciesInput{
				ServiceNamespace: namespace,
				NextToken:        nextToken,
			})
			return callErr
		})
		if err != nil {
			if isThrottleError(err) {
				return policies, c.throttleWarning(namespace, "DescribeScalingPolicies", "scaling_policies"), nil
			}
			return nil, nil, err
		}
		if page == nil {
			return policies, nil, nil
		}
		for _, policy := range page.ScalingPolicies {
			policies = append(policies, mapScalingPolicy(policy))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return policies, nil, nil
		}
	}
}

func (c *Client) listScheduledActions(
	ctx context.Context,
	namespace awsaastypes.ServiceNamespace,
) ([]aasservice.ScheduledAction, *awscloud.WarningObservation, error) {
	var actions []aasservice.ScheduledAction
	var nextToken *string
	for {
		var page *awsaas.DescribeScheduledActionsOutput
		err := c.recordAPICall(ctx, "DescribeScheduledActions", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeScheduledActions(callCtx, &awsaas.DescribeScheduledActionsInput{
				ServiceNamespace: namespace,
				NextToken:        nextToken,
			})
			return callErr
		})
		if err != nil {
			if isThrottleError(err) {
				return actions, c.throttleWarning(namespace, "DescribeScheduledActions", "scheduled_actions"), nil
			}
			return nil, nil, err
		}
		if page == nil {
			return actions, nil, nil
		}
		for _, action := range page.ScheduledActions {
			actions = append(actions, mapScheduledAction(action))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return actions, nil, nil
		}
	}
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

var _ aasservice.Client = (*Client)(nil)

var _ apiClient = (*awsaas.Client)(nil)
