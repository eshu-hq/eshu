// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapprunner "github.com/aws/aws-sdk-go-v2/service/apprunner"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apprunnerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apprunner"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only AWS App Runner read surface used by the
// adapter. Only List and Describe reads appear here; no Create/Delete/Update,
// Pause/Resume/StartDeployment, DeleteConnection, Associate/Disassociate, or
// any other mutation operation is reachable.
type apiClient interface {
	ListServices(context.Context, *awsapprunner.ListServicesInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListServicesOutput, error)
	DescribeService(context.Context, *awsapprunner.DescribeServiceInput, ...func(*awsapprunner.Options)) (*awsapprunner.DescribeServiceOutput, error)
	ListTagsForResource(context.Context, *awsapprunner.ListTagsForResourceInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListTagsForResourceOutput, error)
	ListConnections(context.Context, *awsapprunner.ListConnectionsInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListConnectionsOutput, error)
	ListAutoScalingConfigurations(context.Context, *awsapprunner.ListAutoScalingConfigurationsInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListAutoScalingConfigurationsOutput, error)
	DescribeAutoScalingConfiguration(context.Context, *awsapprunner.DescribeAutoScalingConfigurationInput, ...func(*awsapprunner.Options)) (*awsapprunner.DescribeAutoScalingConfigurationOutput, error)
	ListObservabilityConfigurations(context.Context, *awsapprunner.ListObservabilityConfigurationsInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListObservabilityConfigurationsOutput, error)
	DescribeObservabilityConfiguration(context.Context, *awsapprunner.DescribeObservabilityConfigurationInput, ...func(*awsapprunner.Options)) (*awsapprunner.DescribeObservabilityConfigurationOutput, error)
	ListVpcConnectors(context.Context, *awsapprunner.ListVpcConnectorsInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListVpcConnectorsOutput, error)
	ListVpcIngressConnections(context.Context, *awsapprunner.ListVpcIngressConnectionsInput, ...func(*awsapprunner.Options)) (*awsapprunner.ListVpcIngressConnectionsOutput, error)
	DescribeVpcIngressConnection(context.Context, *awsapprunner.DescribeVpcIngressConnectionInput, ...func(*awsapprunner.Options)) (*awsapprunner.DescribeVpcIngressConnectionOutput, error)
}

// Client adapts the AWS SDK for Go v2 App Runner client into scanner-owned
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an App Runner SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsapprunner.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListServices returns App Runner services visible to the configured AWS
// credentials. Each service summary is enriched through DescribeService so the
// scanner sees the full source, network, instance, encryption, health-check,
// autoscaling, and observability configuration.
func (c *Client) ListServices(ctx context.Context) ([]apprunnerservice.Service, error) {
	var services []apprunnerservice.Service
	var token *string
	for {
		var page *awsapprunner.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListServices(callCtx, &awsapprunner.ListServicesInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.ServiceSummaryList {
			serviceARN := strings.TrimSpace(aws.ToString(summary.ServiceArn))
			if serviceARN == "" {
				continue
			}
			service, err := c.describeService(ctx, serviceARN)
			if err != nil {
				return nil, err
			}
			services = append(services, service)
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return services, nil
}

func (c *Client) describeService(ctx context.Context, serviceARN string) (apprunnerservice.Service, error) {
	var output *awsapprunner.DescribeServiceOutput
	err := c.recordAPICall(ctx, "DescribeService", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeService(callCtx, &awsapprunner.DescribeServiceInput{
			ServiceArn: aws.String(serviceARN),
		})
		return err
	})
	if err != nil {
		return apprunnerservice.Service{}, err
	}
	if output == nil || output.Service == nil {
		return apprunnerservice.Service{ARN: serviceARN}, nil
	}
	tags, err := c.listTags(ctx, serviceARN)
	if err != nil {
		return apprunnerservice.Service{}, err
	}
	return mapService(output.Service, tags), nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	var output *awsapprunner.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsapprunner.ListTagsForResourceInput{
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
	return mapTags(output.Tags), nil
}

// ListConnections returns App Runner connections. The list response carries the
// full connection metadata, so no per-connection describe is needed.
func (c *Client) ListConnections(ctx context.Context) ([]apprunnerservice.Connection, error) {
	var connections []apprunnerservice.Connection
	var token *string
	for {
		var page *awsapprunner.ListConnectionsOutput
		err := c.recordAPICall(ctx, "ListConnections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConnections(callCtx, &awsapprunner.ListConnectionsInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.ConnectionSummaryList {
			connections = append(connections, mapConnection(summary))
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return connections, nil
}

// ListAutoScalingConfigurations returns App Runner automatic scaling
// configuration revisions, enriched through DescribeAutoScalingConfiguration so
// the scanner sees min/max size and concurrency detail.
func (c *Client) ListAutoScalingConfigurations(ctx context.Context) ([]apprunnerservice.AutoScalingConfiguration, error) {
	var configurations []apprunnerservice.AutoScalingConfiguration
	var token *string
	for {
		var page *awsapprunner.ListAutoScalingConfigurationsOutput
		err := c.recordAPICall(ctx, "ListAutoScalingConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAutoScalingConfigurations(callCtx, &awsapprunner.ListAutoScalingConfigurationsInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.AutoScalingConfigurationSummaryList {
			arn := strings.TrimSpace(aws.ToString(summary.AutoScalingConfigurationArn))
			if arn == "" {
				continue
			}
			configuration, err := c.describeAutoScalingConfiguration(ctx, arn)
			if err != nil {
				return nil, err
			}
			configurations = append(configurations, configuration)
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return configurations, nil
}

func (c *Client) describeAutoScalingConfiguration(ctx context.Context, arn string) (apprunnerservice.AutoScalingConfiguration, error) {
	var output *awsapprunner.DescribeAutoScalingConfigurationOutput
	err := c.recordAPICall(ctx, "DescribeAutoScalingConfiguration", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeAutoScalingConfiguration(callCtx, &awsapprunner.DescribeAutoScalingConfigurationInput{
			AutoScalingConfigurationArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return apprunnerservice.AutoScalingConfiguration{}, err
	}
	if output == nil || output.AutoScalingConfiguration == nil {
		return apprunnerservice.AutoScalingConfiguration{ARN: arn}, nil
	}
	return mapAutoScalingConfiguration(output.AutoScalingConfiguration), nil
}

// ListObservabilityConfigurations returns App Runner observability
// configuration revisions, enriched through DescribeObservabilityConfiguration
// so the scanner sees the trace vendor and status.
func (c *Client) ListObservabilityConfigurations(ctx context.Context) ([]apprunnerservice.ObservabilityConfiguration, error) {
	var configurations []apprunnerservice.ObservabilityConfiguration
	var token *string
	for {
		var page *awsapprunner.ListObservabilityConfigurationsOutput
		err := c.recordAPICall(ctx, "ListObservabilityConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListObservabilityConfigurations(callCtx, &awsapprunner.ListObservabilityConfigurationsInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.ObservabilityConfigurationSummaryList {
			arn := strings.TrimSpace(aws.ToString(summary.ObservabilityConfigurationArn))
			if arn == "" {
				continue
			}
			configuration, err := c.describeObservabilityConfiguration(ctx, arn)
			if err != nil {
				return nil, err
			}
			configurations = append(configurations, configuration)
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return configurations, nil
}

func (c *Client) describeObservabilityConfiguration(ctx context.Context, arn string) (apprunnerservice.ObservabilityConfiguration, error) {
	var output *awsapprunner.DescribeObservabilityConfigurationOutput
	err := c.recordAPICall(ctx, "DescribeObservabilityConfiguration", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeObservabilityConfiguration(callCtx, &awsapprunner.DescribeObservabilityConfigurationInput{
			ObservabilityConfigurationArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return apprunnerservice.ObservabilityConfiguration{}, err
	}
	if output == nil || output.ObservabilityConfiguration == nil {
		return apprunnerservice.ObservabilityConfiguration{ARN: arn}, nil
	}
	return mapObservabilityConfiguration(output.ObservabilityConfiguration), nil
}

// ListVpcConnectors returns App Runner VPC connectors. The list response
// carries subnets and security groups, so no per-connector describe is needed.
func (c *Client) ListVpcConnectors(ctx context.Context) ([]apprunnerservice.VpcConnector, error) {
	var connectors []apprunnerservice.VpcConnector
	var token *string
	for {
		var page *awsapprunner.ListVpcConnectorsOutput
		err := c.recordAPICall(ctx, "ListVpcConnectors", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListVpcConnectors(callCtx, &awsapprunner.ListVpcConnectorsInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, connector := range page.VpcConnectors {
			connectors = append(connectors, mapVpcConnector(connector))
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return connectors, nil
}

// ListVpcIngressConnections returns App Runner VPC ingress connections,
// enriched through DescribeVpcIngressConnection so the scanner sees the domain
// name and ingress VPC configuration.
func (c *Client) ListVpcIngressConnections(ctx context.Context) ([]apprunnerservice.VpcIngressConnection, error) {
	var ingressConnections []apprunnerservice.VpcIngressConnection
	var token *string
	for {
		var page *awsapprunner.ListVpcIngressConnectionsOutput
		err := c.recordAPICall(ctx, "ListVpcIngressConnections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListVpcIngressConnections(callCtx, &awsapprunner.ListVpcIngressConnectionsInput{NextToken: token})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.VpcIngressConnectionSummaryList {
			arn := strings.TrimSpace(aws.ToString(summary.VpcIngressConnectionArn))
			if arn == "" {
				continue
			}
			ingress, err := c.describeVpcIngressConnection(ctx, arn)
			if err != nil {
				return nil, err
			}
			ingressConnections = append(ingressConnections, ingress)
		}
		token = page.NextToken
		if token == nil {
			break
		}
	}
	return ingressConnections, nil
}

func (c *Client) describeVpcIngressConnection(ctx context.Context, arn string) (apprunnerservice.VpcIngressConnection, error) {
	var output *awsapprunner.DescribeVpcIngressConnectionOutput
	err := c.recordAPICall(ctx, "DescribeVpcIngressConnection", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeVpcIngressConnection(callCtx, &awsapprunner.DescribeVpcIngressConnectionInput{
			VpcIngressConnectionArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return apprunnerservice.VpcIngressConnection{}, err
	}
	if output == nil || output.VpcIngressConnection == nil {
		return apprunnerservice.VpcIngressConnection{ARN: arn}, nil
	}
	return mapVpcIngressConnection(output.VpcIngressConnection), nil
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

var (
	_ apprunnerservice.Client = (*Client)(nil)
	_ apiClient               = (*awsapprunner.Client)(nil)
)
