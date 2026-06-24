// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnetfw "github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	awsnetfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	netfwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkfirewall"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listLimit bounds each Network Firewall list page. The list APIs are not
// standard paginators, so the adapter loops on NextToken explicitly.
const listLimit int32 = 100

// apiClient is the read-only Network Firewall surface the adapter consumes. It
// exposes only List/Describe operations, and the Describe methods are chosen so
// no rule source (Suricata signature body), policy rule body, or certificate
// body is ever returned: rule group metadata comes from DescribeRuleGroupMetadata
// (not DescribeRuleGroup, whose output carries the rule source). A reflection
// gate in exclusion_test.go fails the build path if any mutation, DescribeRuleGroup,
// or other rule-body method is added here.
type apiClient interface {
	ListFirewalls(context.Context, *awsnetfw.ListFirewallsInput, ...func(*awsnetfw.Options)) (*awsnetfw.ListFirewallsOutput, error)
	DescribeFirewall(context.Context, *awsnetfw.DescribeFirewallInput, ...func(*awsnetfw.Options)) (*awsnetfw.DescribeFirewallOutput, error)
	ListFirewallPolicies(context.Context, *awsnetfw.ListFirewallPoliciesInput, ...func(*awsnetfw.Options)) (*awsnetfw.ListFirewallPoliciesOutput, error)
	DescribeFirewallPolicy(context.Context, *awsnetfw.DescribeFirewallPolicyInput, ...func(*awsnetfw.Options)) (*awsnetfw.DescribeFirewallPolicyOutput, error)
	ListRuleGroups(context.Context, *awsnetfw.ListRuleGroupsInput, ...func(*awsnetfw.Options)) (*awsnetfw.ListRuleGroupsOutput, error)
	DescribeRuleGroupMetadata(context.Context, *awsnetfw.DescribeRuleGroupMetadataInput, ...func(*awsnetfw.Options)) (*awsnetfw.DescribeRuleGroupMetadataOutput, error)
	ListTLSInspectionConfigurations(context.Context, *awsnetfw.ListTLSInspectionConfigurationsInput, ...func(*awsnetfw.Options)) (*awsnetfw.ListTLSInspectionConfigurationsOutput, error)
	DescribeTLSInspectionConfiguration(context.Context, *awsnetfw.DescribeTLSInspectionConfigurationInput, ...func(*awsnetfw.Options)) (*awsnetfw.DescribeTLSInspectionConfigurationOutput, error)
	ListTagsForResource(context.Context, *awsnetfw.ListTagsForResourceInput, ...func(*awsnetfw.Options)) (*awsnetfw.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK for Go v2 Network Firewall reads into scanner-owned
// metadata. It never persists rule group rule sources, firewall policy rule
// bodies, or TLS inspection certificate bodies, and it never calls a Network
// Firewall mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Network Firewall SDK adapter for one claimed AWS boundary.
// Network Firewall is a regional service, so the adapter scans the boundary
// region directly.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsnetfw.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListFirewalls returns firewall metadata with the firewall policy ARN, VPC id,
// and subnet mappings resolved from DescribeFirewall. It never reads any rule
// body.
func (c *Client) ListFirewalls(ctx context.Context) ([]netfwservice.Firewall, error) {
	var firewalls []netfwservice.Firewall
	var token *string
	for {
		var page *awsnetfw.ListFirewallsOutput
		err := c.recordAPICall(ctx, "ListFirewalls", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFirewalls(callCtx, &awsnetfw.ListFirewallsInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return firewalls, nil
		}
		for _, summary := range page.Firewalls {
			firewall, err := c.firewallMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			firewalls = append(firewalls, firewall)
		}
		if token = nextToken(page.NextToken); token == nil {
			return firewalls, nil
		}
	}
}

func (c *Client) firewallMetadata(ctx context.Context, summary awsnetfwtypes.FirewallMetadata) (netfwservice.Firewall, error) {
	var output *awsnetfw.DescribeFirewallOutput
	err := c.recordAPICall(ctx, "DescribeFirewall", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeFirewall(callCtx, &awsnetfw.DescribeFirewallInput{
			FirewallArn: summary.FirewallArn,
		})
		return err
	})
	if err != nil {
		return netfwservice.Firewall{}, err
	}
	return mapFirewall(output), nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var token *string
	for {
		var output *awsnetfw.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsForResource(callCtx, &awsnetfw.ListTagsForResourceInput{
				ResourceArn: aws.String(resourceARN),
				MaxResults:  aws.Int32(listLimit),
				NextToken:   token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
		if token = nextToken(output.NextToken); token == nil {
			break
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func nextToken(token *string) *string {
	if aws.ToString(token) == "" {
		return nil
	}
	return token
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
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") || strings.Contains(code, "rate")
}

var _ netfwservice.Client = (*Client)(nil)
