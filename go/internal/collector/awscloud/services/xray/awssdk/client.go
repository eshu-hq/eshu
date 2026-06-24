// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsxray "github.com/aws/aws-sdk-go-v2/service/xray"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	xrayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/xray"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the narrow AWS X-Ray SDK surface the adapter is allowed to call.
// It is configuration-only by construction: it lists exactly the three X-Ray
// configuration reads (GetGroups, GetSamplingRules, GetEncryptionConfig) and
// nothing else.
//
// It intentionally excludes every observability-payload read — GetTraceSummaries,
// BatchGetTraces, GetTraceGraph, GetServiceGraph, GetTimeSeriesServiceStatistics,
// GetInsight, GetInsightSummaries, GetInsightEvents, GetInsightImpactGraph,
// GetGroup (single-group reads add nothing the list does not), GetSamplingTargets,
// GetSamplingStatisticSummaries — and every mutation — PutTraceSegments,
// PutTelemetryRecords, CreateGroup, UpdateGroup, DeleteGroup, CreateSamplingRule,
// UpdateSamplingRule, DeleteSamplingRule, PutEncryptionConfig. Because the Client
// struct holds an apiClient value rather than the concrete *xray.Client, those
// methods are unreachable from this package.
type apiClient interface {
	GetGroups(
		context.Context,
		*awsxray.GetGroupsInput,
		...func(*awsxray.Options),
	) (*awsxray.GetGroupsOutput, error)
	GetSamplingRules(
		context.Context,
		*awsxray.GetSamplingRulesInput,
		...func(*awsxray.Options),
	) (*awsxray.GetSamplingRulesOutput, error)
	GetEncryptionConfig(
		context.Context,
		*awsxray.GetEncryptionConfigInput,
		...func(*awsxray.Options),
	) (*awsxray.GetEncryptionConfigOutput, error)
}

// Client adapts AWS SDK X-Ray control-plane reads into configuration-only
// scanner records. It never calls a trace, service-graph, insight, telemetry,
// or mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an X-Ray SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsxray.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// GetGroups returns X-Ray group configuration visible to the configured AWS
// credentials. The filter expression is read as group configuration; no trace
// it selects is read.
func (c *Client) GetGroups(ctx context.Context) ([]xrayservice.Group, error) {
	var groups []xrayservice.Group
	var nextToken *string
	for {
		var page *awsxray.GetGroupsOutput
		err := c.recordAPICall(ctx, "GetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetGroups(callCtx, &awsxray.GetGroupsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, raw := range page.Groups {
			groups = append(groups, mapGroup(raw))
		}
		if aws.ToString(page.NextToken) == "" {
			return groups, nil
		}
		nextToken = page.NextToken
	}
}

// GetSamplingRules returns X-Ray sampling rule configuration visible to the
// configured AWS credentials. Only the rule configuration is mapped; no
// sampled request, trace, or summary is read.
func (c *Client) GetSamplingRules(ctx context.Context) ([]xrayservice.SamplingRule, error) {
	var rules []xrayservice.SamplingRule
	var nextToken *string
	for {
		var page *awsxray.GetSamplingRulesOutput
		err := c.recordAPICall(ctx, "GetSamplingRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetSamplingRules(callCtx, &awsxray.GetSamplingRulesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, raw := range page.SamplingRuleRecords {
			if mapped, ok := mapSamplingRule(raw); ok {
				rules = append(rules, mapped)
			}
		}
		if aws.ToString(page.NextToken) == "" {
			return rules, nil
		}
		nextToken = page.NextToken
	}
}

// GetEncryptionConfig returns the account-region X-Ray encryption
// configuration visible to the configured AWS credentials, or nil when AWS
// reports none.
func (c *Client) GetEncryptionConfig(ctx context.Context) (*xrayservice.EncryptionConfig, error) {
	var output *awsxray.GetEncryptionConfigOutput
	err := c.recordAPICall(ctx, "GetEncryptionConfig", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetEncryptionConfig(callCtx, &awsxray.GetEncryptionConfigInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.EncryptionConfig == nil {
		return nil, nil
	}
	config := mapEncryptionConfig(output.EncryptionConfig)
	return &config, nil
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

var _ xrayservice.Client = (*Client)(nil)

var _ apiClient = (*awsxray.Client)(nil)
