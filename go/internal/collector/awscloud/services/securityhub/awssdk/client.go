// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecurityhub "github.com/aws/aws-sdk-go-v2/service/securityhub"
	awssecurityhubtypes "github.com/aws/aws-sdk-go-v2/service/securityhub/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultPageSize   int32 = 100
	unspecifiedBucket       = "UNSPECIFIED"
)

type apiClient interface {
	DescribeHub(
		context.Context,
		*awssecurityhub.DescribeHubInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.DescribeHubOutput, error)
	GetAdministratorAccount(
		context.Context,
		*awssecurityhub.GetAdministratorAccountInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.GetAdministratorAccountOutput, error)
	ListMembers(
		context.Context,
		*awssecurityhub.ListMembersInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.ListMembersOutput, error)
	GetEnabledStandards(
		context.Context,
		*awssecurityhub.GetEnabledStandardsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.GetEnabledStandardsOutput, error)
	DescribeStandardsControls(
		context.Context,
		*awssecurityhub.DescribeStandardsControlsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.DescribeStandardsControlsOutput, error)
	DescribeActionTargets(
		context.Context,
		*awssecurityhub.DescribeActionTargetsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.DescribeActionTargetsOutput, error)
	GetInsights(
		context.Context,
		*awssecurityhub.GetInsightsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.GetInsightsOutput, error)
	GetInsightResults(
		context.Context,
		*awssecurityhub.GetInsightResultsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.GetInsightResultsOutput, error)
	GetFindings(
		context.Context,
		*awssecurityhub.GetFindingsInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.GetFindingsOutput, error)
	ListTagsForResource(
		context.Context,
		*awssecurityhub.ListTagsForResourceInput,
		...func(*awssecurityhub.Options),
	) (*awssecurityhub.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Security Hub read calls into scanner-owned metadata.
// It never calls mutation APIs or emits finding bodies.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Security Hub SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssecurityhub.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Security Hub metadata visible to the configured AWS
// credentials. Finding bodies are reduced in-process to bounded aggregate
// posture counts and are not returned.
func (c *Client) Snapshot(ctx context.Context) (securityhubservice.Snapshot, error) {
	hub, err := c.describeHub(ctx)
	if err != nil {
		return securityhubservice.Snapshot{}, err
	}
	administrator, err := c.getAdministrator(ctx)
	if err != nil {
		if !isStandaloneMembershipError(err) {
			return securityhubservice.Snapshot{}, err
		}
	} else {
		hub.AdministratorAccountID = administrator.AccountID
		hub.AdministratorStatus = administrator.Status
	}
	members, err := c.listMembers(ctx)
	switch {
	case err == nil:
		if len(members) > 0 {
			hub.MemberEnumerationStatus = "administrator"
		}
	case isStandaloneMembershipError(err):
		if hub.AdministratorAccountID != "" {
			hub.MemberEnumerationStatus = "member"
			break
		}
		hub.MemberEnumerationStatus = "standalone"
	default:
		return securityhubservice.Snapshot{}, err
	}
	standards, err := c.listStandards(ctx)
	if err != nil {
		return securityhubservice.Snapshot{}, err
	}
	actionTargets, err := c.listActionTargets(ctx)
	if err != nil {
		return securityhubservice.Snapshot{}, err
	}
	insights, err := c.listInsights(ctx)
	if err != nil {
		return securityhubservice.Snapshot{}, err
	}
	findingCounts, err := c.listFindingCounts(ctx)
	if err != nil {
		return securityhubservice.Snapshot{}, err
	}
	applyComplianceCounts(standards, findingCounts)
	return securityhubservice.Snapshot{
		Hub:           hub,
		Members:       members,
		Standards:     standards,
		ActionTargets: actionTargets,
		Insights:      insights,
		FindingCounts: findingCounts,
	}, nil
}

func (c *Client) describeHub(ctx context.Context) (securityhubservice.Hub, error) {
	var output *awssecurityhub.DescribeHubOutput
	err := c.recordAPICall(ctx, "DescribeHub", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeHub(callCtx, &awssecurityhub.DescribeHubInput{})
		return err
	})
	if err != nil {
		return securityhubservice.Hub{}, fmt.Errorf("describe Security Hub hub: %w", err)
	}
	if output == nil {
		return securityhubservice.Hub{}, nil
	}
	hub := mapHub(output)
	tags, err := c.listTags(ctx, hub.ARN)
	if err != nil {
		return securityhubservice.Hub{}, err
	}
	hub.Tags = tags
	return hub, nil
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

func isStandaloneMembershipError(err error) bool {
	var invalidAccess *awssecurityhubtypes.InvalidAccessException
	if errors.As(err, &invalidAccess) {
		return true
	}
	var notFound *awssecurityhubtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

var _ securityhubservice.Client = (*Client)(nil)

var _ apiClient = (*awssecurityhub.Client)(nil)
