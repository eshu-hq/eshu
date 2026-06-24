// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmwaa "github.com/aws/aws-sdk-go-v2/service/mwaa"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	mwaaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mwaa"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the read-only subset of the AWS SDK MWAA client the adapter
// uses. It exposes ListEnvironments and GetEnvironment only. CreateEnvironment,
// UpdateEnvironment, DeleteEnvironment, CreateCliToken, CreateWebLoginToken,
// InvokeRestApi, PublishMetrics, TagResource, and UntagResource are absent by
// construction, so the adapter cannot mutate an environment or mint an Airflow
// access token.
type apiClient interface {
	ListEnvironments(context.Context, *awsmwaa.ListEnvironmentsInput, ...func(*awsmwaa.Options)) (*awsmwaa.ListEnvironmentsOutput, error)
	GetEnvironment(context.Context, *awsmwaa.GetEnvironmentInput, ...func(*awsmwaa.Options)) (*awsmwaa.GetEnvironmentOutput, error)
}

// Client adapts AWS SDK MWAA pagination and point reads into scanner-owned
// metadata. The adapter never reads Apache Airflow configuration option
// values, connection strings, or any secret, and never calls a mutation or
// token API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an MWAA SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsmwaa.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListEnvironments enumerates MWAA environment names with ListEnvironments and
// reads each one with GetEnvironment for safe metadata. Apache Airflow
// configuration option values reported on the environment are never mapped, so
// they never leave the adapter.
func (c *Client) ListEnvironments(ctx context.Context) ([]mwaaservice.Environment, error) {
	names, err := c.listEnvironmentNames(ctx)
	if err != nil {
		return nil, err
	}
	environments := make([]mwaaservice.Environment, 0, len(names))
	for _, name := range names {
		environment, err := c.getEnvironment(ctx, name)
		if err != nil {
			return nil, err
		}
		if environment == nil {
			continue
		}
		environments = append(environments, *environment)
	}
	return environments, nil
}

func (c *Client) listEnvironmentNames(ctx context.Context) ([]string, error) {
	var names []string
	var nextToken *string
	for {
		var page *awsmwaa.ListEnvironmentsOutput
		err := c.recordAPICall(ctx, "ListEnvironments", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEnvironments(callCtx, &awsmwaa.ListEnvironmentsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, name := range page.Environments {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return names, nil
		}
	}
}

func (c *Client) getEnvironment(ctx context.Context, name string) (*mwaaservice.Environment, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	var output *awsmwaa.GetEnvironmentOutput
	err := c.recordAPICall(ctx, "GetEnvironment", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetEnvironment(callCtx, &awsmwaa.GetEnvironmentInput{
			Name: aws.String(trimmed),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Environment == nil {
		return &mwaaservice.Environment{Name: trimmed}, nil
	}
	environment := mapEnvironment(*output.Environment)
	if strings.TrimSpace(environment.Name) == "" {
		environment.Name = trimmed
	}
	return &environment, nil
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

var _ mwaaservice.Client = (*Client)(nil)

var _ apiClient = (*awsmwaa.Client)(nil)
