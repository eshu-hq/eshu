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
	tgwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/transitgateway"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const transitGatewayPageLimit int32 = 1000

// apiClient lists only the read-side EC2 Transit Gateway operations the scanner
// needs. No mutation method (Create*/Delete*/Modify*/Associate*/Disassociate*/
// Enable*/Disable*/Accept*/Reject*/Replace*/Register*/Deregister*) is embedded
// here, and the package test pins that contract by reflecting over this
// interface. Embedding a new AWS SDK paginator interface that drags in a
// mutation method fails the test before the change can be compiled into
// production.
type apiClient interface {
	awsec2.DescribeTransitGatewaysAPIClient
	awsec2.DescribeTransitGatewayRouteTablesAPIClient
	awsec2.DescribeTransitGatewayAttachmentsAPIClient
	awsec2.DescribeTransitGatewayPeeringAttachmentsAPIClient
	awsec2.DescribeTransitGatewayMulticastDomainsAPIClient
	awsec2.DescribeTransitGatewayPolicyTablesAPIClient
}

// Client adapts AWS SDK EC2 Transit Gateway pagination into scanner-owned
// records. It holds no mutable cross-call state; the AWS SDK client is
// constructed per claim by the runtimebind builder.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Transit Gateway SDK adapter for one claimed AWS boundary.
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

// ListTransitGateways returns transit gateways visible to the configured AWS
// credentials.
func (c *Client) ListTransitGateways(ctx context.Context) ([]tgwservice.TransitGateway, error) {
	paginator := awsec2.NewDescribeTransitGatewaysPaginator(c.client, &awsec2.DescribeTransitGatewaysInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var gateways []tgwservice.TransitGateway
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewaysOutput
		err := c.recordAPICall(ctx, "DescribeTransitGateways", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, gateway := range page.TransitGateways {
			gateways = append(gateways, mapTransitGateway(gateway))
		}
	}
	return gateways, nil
}

// ListTransitGatewayRouteTables returns transit gateway route tables visible to
// the configured AWS credentials.
func (c *Client) ListTransitGatewayRouteTables(ctx context.Context) ([]tgwservice.RouteTable, error) {
	paginator := awsec2.NewDescribeTransitGatewayRouteTablesPaginator(c.client, &awsec2.DescribeTransitGatewayRouteTablesInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var routeTables []tgwservice.RouteTable
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewayRouteTablesOutput
		err := c.recordAPICall(ctx, "DescribeTransitGatewayRouteTables", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, routeTable := range page.TransitGatewayRouteTables {
			routeTables = append(routeTables, mapRouteTable(routeTable))
		}
	}
	return routeTables, nil
}

// ListTransitGatewayAttachments returns transit gateway attachments visible to
// the configured AWS credentials. AWS reports VPC, VPN, Direct Connect gateway,
// peering, and Connect attachments through this one API.
func (c *Client) ListTransitGatewayAttachments(ctx context.Context) ([]tgwservice.Attachment, error) {
	paginator := awsec2.NewDescribeTransitGatewayAttachmentsPaginator(c.client, &awsec2.DescribeTransitGatewayAttachmentsInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var attachments []tgwservice.Attachment
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewayAttachmentsOutput
		err := c.recordAPICall(ctx, "DescribeTransitGatewayAttachments", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, attachment := range page.TransitGatewayAttachments {
			attachments = append(attachments, mapAttachment(attachment))
		}
	}
	return attachments, nil
}

// ListTransitGatewayPeeringAttachments returns transit gateway peering
// attachments visible to the configured AWS credentials.
func (c *Client) ListTransitGatewayPeeringAttachments(ctx context.Context) ([]tgwservice.PeeringAttachment, error) {
	paginator := awsec2.NewDescribeTransitGatewayPeeringAttachmentsPaginator(c.client, &awsec2.DescribeTransitGatewayPeeringAttachmentsInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var peerings []tgwservice.PeeringAttachment
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewayPeeringAttachmentsOutput
		err := c.recordAPICall(ctx, "DescribeTransitGatewayPeeringAttachments", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, peering := range page.TransitGatewayPeeringAttachments {
			peerings = append(peerings, mapPeeringAttachment(peering))
		}
	}
	return peerings, nil
}

// ListTransitGatewayMulticastDomains returns transit gateway multicast domains
// visible to the configured AWS credentials.
func (c *Client) ListTransitGatewayMulticastDomains(ctx context.Context) ([]tgwservice.MulticastDomain, error) {
	paginator := awsec2.NewDescribeTransitGatewayMulticastDomainsPaginator(c.client, &awsec2.DescribeTransitGatewayMulticastDomainsInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var domains []tgwservice.MulticastDomain
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewayMulticastDomainsOutput
		err := c.recordAPICall(ctx, "DescribeTransitGatewayMulticastDomains", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, domain := range page.TransitGatewayMulticastDomains {
			domains = append(domains, mapMulticastDomain(domain))
		}
	}
	return domains, nil
}

// ListTransitGatewayPolicyTables returns transit gateway policy tables visible
// to the configured AWS credentials. The adapter never calls
// GetTransitGatewayPolicyTableEntries; only identity and state are read.
func (c *Client) ListTransitGatewayPolicyTables(ctx context.Context) ([]tgwservice.PolicyTable, error) {
	paginator := awsec2.NewDescribeTransitGatewayPolicyTablesPaginator(c.client, &awsec2.DescribeTransitGatewayPolicyTablesInput{
		MaxResults: aws.Int32(transitGatewayPageLimit),
	})
	var policyTables []tgwservice.PolicyTable
	for paginator.HasMorePages() {
		var page *awsec2.DescribeTransitGatewayPolicyTablesOutput
		err := c.recordAPICall(ctx, "DescribeTransitGatewayPolicyTables", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, policyTable := range page.TransitGatewayPolicyTables {
			policyTables = append(policyTables, mapPolicyTable(policyTable))
		}
	}
	return policyTables, nil
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

var _ tgwservice.Client = (*Client)(nil)

var _ apiClient = (*awsec2.Client)(nil)
