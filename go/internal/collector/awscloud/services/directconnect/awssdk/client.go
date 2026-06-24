// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdx "github.com/aws/aws-sdk-go-v2/service/directconnect"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	dxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/directconnect"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const directConnectPageLimit int32 = 100

// apiClient lists only the read-side Direct Connect operations the scanner
// needs. No mutation method (Create*/Delete*/Update*/Associate*/Disassociate*/
// Confirm*/Allocate*/Accept*/Tag*/Untag*/Start*/Stop*) is named here, and the
// package test pins that contract by reflecting over this interface. The
// adapter also intentionally omits DescribeRouterConfiguration: that operation
// renders the BGP authentication key into the returned router configuration.
type apiClient interface {
	DescribeConnections(context.Context, *awsdx.DescribeConnectionsInput, ...func(*awsdx.Options)) (*awsdx.DescribeConnectionsOutput, error)
	DescribeVirtualInterfaces(context.Context, *awsdx.DescribeVirtualInterfacesInput, ...func(*awsdx.Options)) (*awsdx.DescribeVirtualInterfacesOutput, error)
	DescribeDirectConnectGateways(context.Context, *awsdx.DescribeDirectConnectGatewaysInput, ...func(*awsdx.Options)) (*awsdx.DescribeDirectConnectGatewaysOutput, error)
	DescribeLags(context.Context, *awsdx.DescribeLagsInput, ...func(*awsdx.Options)) (*awsdx.DescribeLagsOutput, error)
	DescribeDirectConnectGatewayAssociations(context.Context, *awsdx.DescribeDirectConnectGatewayAssociationsInput, ...func(*awsdx.Options)) (*awsdx.DescribeDirectConnectGatewayAssociationsOutput, error)
}

// Client adapts AWS SDK Direct Connect responses into scanner-owned records. It
// holds no mutable cross-call state; the AWS SDK client is constructed per
// claim by the runtimebind builder.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Direct Connect SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdx.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListConnections returns Direct Connect connections visible to the configured
// AWS credentials. The Direct Connect Describe operations carry NextToken in
// their contract, so the adapter follows it even though the service often
// returns the full list in one page.
func (c *Client) ListConnections(ctx context.Context) ([]dxservice.Connection, error) {
	var connections []dxservice.Connection
	var token *string
	for {
		var page *awsdx.DescribeConnectionsOutput
		err := c.recordAPICall(ctx, "DescribeConnections", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeConnections(callCtx, &awsdx.DescribeConnectionsInput{
				MaxResults: aws.Int32(directConnectPageLimit),
				NextToken:  token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		for _, connection := range page.Connections {
			connections = append(connections, mapConnection(connection))
		}
		token = nextToken(page.NextToken)
		if token == nil {
			break
		}
	}
	return connections, nil
}

// ListVirtualInterfaces returns Direct Connect virtual interfaces visible to
// the configured AWS credentials. The BGP authentication key AWS returns is
// dropped by the mapper.
func (c *Client) ListVirtualInterfaces(ctx context.Context) ([]dxservice.VirtualInterface, error) {
	var interfaces []dxservice.VirtualInterface
	var token *string
	for {
		var page *awsdx.DescribeVirtualInterfacesOutput
		err := c.recordAPICall(ctx, "DescribeVirtualInterfaces", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeVirtualInterfaces(callCtx, &awsdx.DescribeVirtualInterfacesInput{
				MaxResults: aws.Int32(directConnectPageLimit),
				NextToken:  token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		for _, vif := range page.VirtualInterfaces {
			interfaces = append(interfaces, mapVirtualInterface(vif))
		}
		token = nextToken(page.NextToken)
		if token == nil {
			break
		}
	}
	return interfaces, nil
}

// ListGateways returns Direct Connect gateways visible to the configured AWS
// credentials.
func (c *Client) ListGateways(ctx context.Context) ([]dxservice.Gateway, error) {
	var gateways []dxservice.Gateway
	var token *string
	for {
		var page *awsdx.DescribeDirectConnectGatewaysOutput
		err := c.recordAPICall(ctx, "DescribeDirectConnectGateways", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeDirectConnectGateways(callCtx, &awsdx.DescribeDirectConnectGatewaysInput{
				MaxResults: aws.Int32(directConnectPageLimit),
				NextToken:  token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		for _, gateway := range page.DirectConnectGateways {
			gateways = append(gateways, mapGateway(gateway))
		}
		token = nextToken(page.NextToken)
		if token == nil {
			break
		}
	}
	return gateways, nil
}

// ListLAGs returns Direct Connect link aggregation groups visible to the
// configured AWS credentials. MACsec key material AWS returns is dropped by the
// mapper.
func (c *Client) ListLAGs(ctx context.Context) ([]dxservice.LAG, error) {
	var lags []dxservice.LAG
	var token *string
	for {
		var page *awsdx.DescribeLagsOutput
		err := c.recordAPICall(ctx, "DescribeLags", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeLags(callCtx, &awsdx.DescribeLagsInput{
				MaxResults: aws.Int32(directConnectPageLimit),
				NextToken:  token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		for _, lag := range page.Lags {
			lags = append(lags, mapLAG(lag))
		}
		token = nextToken(page.NextToken)
		if token == nil {
			break
		}
	}
	return lags, nil
}

// ListGatewayAssociations returns every Direct Connect gateway association
// visible to the configured AWS credentials. The unscoped describe returns
// associations across all gateways; the adapter follows NextToken until the
// service stops returning one.
func (c *Client) ListGatewayAssociations(ctx context.Context) ([]dxservice.GatewayAssociation, error) {
	var associations []dxservice.GatewayAssociation
	var token *string
	for {
		var page *awsdx.DescribeDirectConnectGatewayAssociationsOutput
		err := c.recordAPICall(ctx, "DescribeDirectConnectGatewayAssociations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeDirectConnectGatewayAssociations(callCtx, &awsdx.DescribeDirectConnectGatewayAssociationsInput{
				MaxResults: aws.Int32(directConnectPageLimit),
				NextToken:  token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		for _, association := range page.DirectConnectGatewayAssociations {
			associations = append(associations, mapGatewayAssociation(association))
		}
		token = nextToken(page.NextToken)
		if token == nil {
			break
		}
	}
	return associations, nil
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

// nextToken normalizes the SDK NextToken into a follow-up token. An empty
// string is treated as no more pages so the adapter does not loop forever on a
// service that returns "" instead of nil.
func nextToken(token *string) *string {
	if token == nil || strings.TrimSpace(aws.ToString(token)) == "" {
		return nil
	}
	return token
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

var _ dxservice.Client = (*Client)(nil)

var _ apiClient = (*awsdx.Client)(nil)
