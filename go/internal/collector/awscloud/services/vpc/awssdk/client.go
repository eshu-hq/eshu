// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	vpcservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpc"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const vpcPageLimit int32 = 1000

// apiClient lists only the read-side EC2 operations the VPC scanner needs.
// No mutation method (Create*/Delete*/Modify*/Associate*/Disassociate*/
// Authorize*/Revoke*/Allocate*/Release*/Replace*/Accept*/Reject*/Attach*/
// Detach*) is embedded here, and the package test pins that contract.
type apiClient interface {
	awsec2.DescribeRouteTablesAPIClient
	awsec2.DescribeInternetGatewaysAPIClient
	awsec2.DescribeNatGatewaysAPIClient
	awsec2.DescribeNetworkAclsAPIClient
	awsec2.DescribeVpcPeeringConnectionsAPIClient
	awsec2.DescribeVpcEndpointsAPIClient
	awsec2.DescribeDhcpOptionsAPIClient
	DescribeAddresses(context.Context, *awsec2.DescribeAddressesInput, ...func(*awsec2.Options)) (*awsec2.DescribeAddressesOutput, error)
	DescribeCustomerGateways(context.Context, *awsec2.DescribeCustomerGatewaysInput, ...func(*awsec2.Options)) (*awsec2.DescribeCustomerGatewaysOutput, error)
	DescribeVpnGateways(context.Context, *awsec2.DescribeVpnGatewaysInput, ...func(*awsec2.Options)) (*awsec2.DescribeVpnGatewaysOutput, error)
	DescribeVpnConnections(context.Context, *awsec2.DescribeVpnConnectionsInput, ...func(*awsec2.Options)) (*awsec2.DescribeVpnConnectionsOutput, error)
}

// Client adapts AWS SDK EC2 pagination into scanner-owned VPC topology
// records. It holds no mutable cross-call state; the AWS-SDK Client is
// constructed per claim by the runtimebind builder.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a VPC SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsec2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListRouteTables returns route tables visible to the configured AWS
// credentials.
func (c *Client) ListRouteTables(ctx context.Context) ([]vpcservice.RouteTable, error) {
	paginator := awsec2.NewDescribeRouteTablesPaginator(c.client, &awsec2.DescribeRouteTablesInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var routeTables []vpcservice.RouteTable
	for paginator.HasMorePages() {
		var page *awsec2.DescribeRouteTablesOutput
		err := c.recordAPICall(ctx, "DescribeRouteTables", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, rt := range page.RouteTables {
			routeTables = append(routeTables, mapRouteTable(rt))
		}
	}
	return routeTables, nil
}

// ListInternetGateways returns internet gateways visible to the configured AWS
// credentials.
func (c *Client) ListInternetGateways(ctx context.Context) ([]vpcservice.InternetGateway, error) {
	paginator := awsec2.NewDescribeInternetGatewaysPaginator(c.client, &awsec2.DescribeInternetGatewaysInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var gateways []vpcservice.InternetGateway
	for paginator.HasMorePages() {
		var page *awsec2.DescribeInternetGatewaysOutput
		err := c.recordAPICall(ctx, "DescribeInternetGateways", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, gateway := range page.InternetGateways {
			gateways = append(gateways, mapInternetGateway(gateway))
		}
	}
	return gateways, nil
}

// ListNATGateways returns NAT gateways visible to the configured AWS
// credentials.
func (c *Client) ListNATGateways(ctx context.Context) ([]vpcservice.NATGateway, error) {
	paginator := awsec2.NewDescribeNatGatewaysPaginator(c.client, &awsec2.DescribeNatGatewaysInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var gateways []vpcservice.NATGateway
	for paginator.HasMorePages() {
		var page *awsec2.DescribeNatGatewaysOutput
		err := c.recordAPICall(ctx, "DescribeNatGateways", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, gateway := range page.NatGateways {
			gateways = append(gateways, mapNATGateway(gateway))
		}
	}
	return gateways, nil
}

// ListNetworkACLs returns network ACLs visible to the configured AWS
// credentials.
func (c *Client) ListNetworkACLs(ctx context.Context) ([]vpcservice.NetworkACL, error) {
	paginator := awsec2.NewDescribeNetworkAclsPaginator(c.client, &awsec2.DescribeNetworkAclsInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var networkACLs []vpcservice.NetworkACL
	for paginator.HasMorePages() {
		var page *awsec2.DescribeNetworkAclsOutput
		err := c.recordAPICall(ctx, "DescribeNetworkAcls", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, networkACL := range page.NetworkAcls {
			networkACLs = append(networkACLs, mapNetworkACL(networkACL))
		}
	}
	return networkACLs, nil
}

// ListVPCPeeringConnections returns VPC peering connections visible to the
// configured AWS credentials.
func (c *Client) ListVPCPeeringConnections(ctx context.Context) ([]vpcservice.VPCPeeringConnection, error) {
	paginator := awsec2.NewDescribeVpcPeeringConnectionsPaginator(c.client, &awsec2.DescribeVpcPeeringConnectionsInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var peerings []vpcservice.VPCPeeringConnection
	for paginator.HasMorePages() {
		var page *awsec2.DescribeVpcPeeringConnectionsOutput
		err := c.recordAPICall(ctx, "DescribeVpcPeeringConnections", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, peering := range page.VpcPeeringConnections {
			peerings = append(peerings, mapVPCPeeringConnection(peering))
		}
	}
	return peerings, nil
}

// ListVPCEndpoints returns VPC endpoints visible to the configured AWS
// credentials.
func (c *Client) ListVPCEndpoints(ctx context.Context) ([]vpcservice.VPCEndpoint, error) {
	paginator := awsec2.NewDescribeVpcEndpointsPaginator(c.client, &awsec2.DescribeVpcEndpointsInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var endpoints []vpcservice.VPCEndpoint
	for paginator.HasMorePages() {
		var page *awsec2.DescribeVpcEndpointsOutput
		err := c.recordAPICall(ctx, "DescribeVpcEndpoints", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, endpoint := range page.VpcEndpoints {
			endpoints = append(endpoints, mapVPCEndpoint(endpoint))
		}
	}
	return endpoints, nil
}

// ListDHCPOptions returns DHCP option sets visible to the configured AWS
// credentials.
func (c *Client) ListDHCPOptions(ctx context.Context) ([]vpcservice.DHCPOptions, error) {
	paginator := awsec2.NewDescribeDhcpOptionsPaginator(c.client, &awsec2.DescribeDhcpOptionsInput{
		MaxResults: aws.Int32(vpcPageLimit),
	})
	var options []vpcservice.DHCPOptions
	for paginator.HasMorePages() {
		var page *awsec2.DescribeDhcpOptionsOutput
		err := c.recordAPICall(ctx, "DescribeDhcpOptions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, option := range page.DhcpOptions {
			options = append(options, mapDHCPOptions(option))
		}
	}
	return options, nil
}

// ListElasticIPs returns VPC-domain Elastic IPs visible to the configured AWS
// credentials. DescribeAddresses does not paginate; AWS returns the full set
// in one response.
func (c *Client) ListElasticIPs(ctx context.Context) ([]vpcservice.ElasticIP, error) {
	var output *awsec2.DescribeAddressesOutput
	err := c.recordAPICall(ctx, "DescribeAddresses", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeAddresses(callCtx, &awsec2.DescribeAddressesInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	result := make([]vpcservice.ElasticIP, 0, len(output.Addresses))
	for _, address := range output.Addresses {
		result = append(result, mapElasticIP(address))
	}
	return result, nil
}

// ListCustomerGateways returns customer gateways visible to the configured AWS
// credentials. DescribeCustomerGateways does not paginate.
func (c *Client) ListCustomerGateways(ctx context.Context) ([]vpcservice.CustomerGateway, error) {
	var output *awsec2.DescribeCustomerGatewaysOutput
	err := c.recordAPICall(ctx, "DescribeCustomerGateways", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeCustomerGateways(callCtx, &awsec2.DescribeCustomerGatewaysInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	result := make([]vpcservice.CustomerGateway, 0, len(output.CustomerGateways))
	for _, gateway := range output.CustomerGateways {
		result = append(result, mapCustomerGateway(gateway))
	}
	return result, nil
}

// ListVPNGateways returns virtual private gateways visible to the configured
// AWS credentials. DescribeVpnGateways does not paginate.
func (c *Client) ListVPNGateways(ctx context.Context) ([]vpcservice.VPNGateway, error) {
	var output *awsec2.DescribeVpnGatewaysOutput
	err := c.recordAPICall(ctx, "DescribeVpnGateways", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeVpnGateways(callCtx, &awsec2.DescribeVpnGatewaysInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	result := make([]vpcservice.VPNGateway, 0, len(output.VpnGateways))
	for _, gateway := range output.VpnGateways {
		result = append(result, mapVPNGateway(gateway))
	}
	return result, nil
}

// ListVPNConnections returns site-to-site VPN connections visible to the
// configured AWS credentials. DescribeVpnConnections does not paginate.
func (c *Client) ListVPNConnections(ctx context.Context) ([]vpcservice.VPNConnection, error) {
	var output *awsec2.DescribeVpnConnectionsOutput
	err := c.recordAPICall(ctx, "DescribeVpnConnections", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeVpnConnections(callCtx, &awsec2.DescribeVpnConnectionsInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	result := make([]vpcservice.VPNConnection, 0, len(output.VpnConnections))
	for _, connection := range output.VpnConnections {
		result = append(result, mapVPNConnection(connection))
	}
	return result, nil
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

var _ vpcservice.Client = (*Client)(nil)

var _ apiClient = (*awsec2.Client)(nil)
