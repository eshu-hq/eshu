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
	ec2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const ec2PageLimit int32 = 1000

type apiClient interface {
	awsec2.DescribeInstancesAPIClient
	awsec2.DescribeNetworkInterfacesAPIClient
	awsec2.DescribeSecurityGroupRulesAPIClient
	awsec2.DescribeSecurityGroupsAPIClient
	awsec2.DescribeSubnetsAPIClient
	awsec2.DescribeVolumesAPIClient
	awsec2.DescribeVpcsAPIClient
}

// Client adapts AWS SDK EC2 pagination into scanner-owned EC2 network records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an EC2 SDK adapter for one claimed AWS boundary.
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

// ListVPCs returns all VPCs visible to the configured AWS credentials.
func (c *Client) ListVPCs(ctx context.Context) ([]ec2service.VPC, error) {
	paginator := awsec2.NewDescribeVpcsPaginator(c.client, &awsec2.DescribeVpcsInput{
		MaxResults: aws.Int32(ec2PageLimit),
	})
	var vpcs []ec2service.VPC
	for paginator.HasMorePages() {
		var page *awsec2.DescribeVpcsOutput
		err := c.recordAPICall(ctx, "DescribeVpcs", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, vpc := range page.Vpcs {
			vpcs = append(vpcs, mapVPC(vpc))
		}
	}
	return vpcs, nil
}

// ListSubnets returns all subnets visible to the configured AWS credentials.
func (c *Client) ListSubnets(ctx context.Context) ([]ec2service.Subnet, error) {
	paginator := awsec2.NewDescribeSubnetsPaginator(c.client, &awsec2.DescribeSubnetsInput{
		MaxResults: aws.Int32(ec2PageLimit),
	})
	var subnets []ec2service.Subnet
	for paginator.HasMorePages() {
		var page *awsec2.DescribeSubnetsOutput
		err := c.recordAPICall(ctx, "DescribeSubnets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, subnet := range page.Subnets {
			subnets = append(subnets, mapSubnet(subnet))
		}
	}
	return subnets, nil
}

// ListSecurityGroups returns all security groups visible to the configured AWS
// credentials.
func (c *Client) ListSecurityGroups(ctx context.Context) ([]ec2service.SecurityGroup, error) {
	paginator := awsec2.NewDescribeSecurityGroupsPaginator(c.client, &awsec2.DescribeSecurityGroupsInput{
		MaxResults: aws.Int32(ec2PageLimit),
	})
	var groups []ec2service.SecurityGroup
	for paginator.HasMorePages() {
		var page *awsec2.DescribeSecurityGroupsOutput
		err := c.recordAPICall(ctx, "DescribeSecurityGroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, group := range page.SecurityGroups {
			groups = append(groups, mapSecurityGroup(group))
		}
	}
	return groups, nil
}

// ListSecurityGroupRules returns all security group rules visible to the
// configured AWS credentials.
func (c *Client) ListSecurityGroupRules(ctx context.Context) ([]ec2service.SecurityGroupRule, error) {
	paginator := awsec2.NewDescribeSecurityGroupRulesPaginator(
		c.client,
		&awsec2.DescribeSecurityGroupRulesInput{MaxResults: aws.Int32(ec2PageLimit)},
	)
	var rules []ec2service.SecurityGroupRule
	for paginator.HasMorePages() {
		var page *awsec2.DescribeSecurityGroupRulesOutput
		err := c.recordAPICall(ctx, "DescribeSecurityGroupRules", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, rule := range page.SecurityGroupRules {
			rules = append(rules, mapSecurityGroupRule(rule))
		}
	}
	return rules, nil
}

// ListNetworkInterfaces returns all network interfaces visible to the
// configured AWS credentials, including AWS-managed ENIs when the account
// allows managed-resource visibility.
func (c *Client) ListNetworkInterfaces(ctx context.Context) ([]ec2service.NetworkInterface, error) {
	paginator := awsec2.NewDescribeNetworkInterfacesPaginator(c.client, networkInterfacesInput())
	var networkInterfaces []ec2service.NetworkInterface
	for paginator.HasMorePages() {
		var page *awsec2.DescribeNetworkInterfacesOutput
		err := c.recordAPICall(ctx, "DescribeNetworkInterfaces", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, networkInterface := range page.NetworkInterfaces {
			networkInterfaces = append(
				networkInterfaces,
				mapNetworkInterface(c.boundary.Region, c.boundary.AccountID, networkInterface),
			)
		}
	}
	return networkInterfaces, nil
}

// ListInstances returns instance posture inputs from the existing
// DescribeInstances pass. The mapper reads only metadata-only posture fields
// (IMDS settings, derived booleans, instance-profile ARN, and block-device
// metadata); it never fetches user-data content, console output, or any other
// instance payload, so this adds no per-instance API fan-out.
func (c *Client) ListInstances(ctx context.Context) ([]ec2service.Instance, error) {
	paginator := awsec2.NewDescribeInstancesPaginator(c.client, &awsec2.DescribeInstancesInput{
		MaxResults: aws.Int32(ec2PageLimit),
	})
	var instances []ec2service.Instance
	for paginator.HasMorePages() {
		var page *awsec2.DescribeInstancesOutput
		err := c.recordAPICall(ctx, "DescribeInstances", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				instances = append(instances, mapInstance(c.boundary.Region, c.boundary.AccountID, instance))
			}
		}
	}
	return instances, nil
}

// ListVolumes returns EBS volume metadata from one boundary-scoped
// DescribeVolumes pass. It does not fill instance posture inline, so EC2
// instance block-device facts remain sourced only by DescribeInstances.
func (c *Client) ListVolumes(ctx context.Context) ([]ec2service.Volume, error) {
	paginator := awsec2.NewDescribeVolumesPaginator(c.client, &awsec2.DescribeVolumesInput{
		MaxResults: aws.Int32(ec2PageLimit),
	})
	var volumes []ec2service.Volume
	for paginator.HasMorePages() {
		var page *awsec2.DescribeVolumesOutput
		err := c.recordAPICall(ctx, "DescribeVolumes", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, volume := range page.Volumes {
			volumes = append(volumes, mapVolume(c.boundary.Region, c.boundary.AccountID, volume))
		}
	}
	return volumes, nil
}

func networkInterfacesInput() *awsec2.DescribeNetworkInterfacesInput {
	return &awsec2.DescribeNetworkInterfacesInput{
		IncludeManagedResources: aws.Bool(true),
		MaxResults:              aws.Int32(ec2PageLimit),
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

var _ ec2service.Client = (*Client)(nil)

var _ apiClient = (*awsec2.Client)(nil)
