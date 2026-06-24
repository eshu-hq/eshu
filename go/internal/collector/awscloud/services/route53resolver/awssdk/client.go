// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsr53r "github.com/aws/aws-sdk-go-v2/service/route53resolver"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	r53rservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53resolver"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only Route 53 Resolver read surface the adapter
// depends on. It is composed of List and Get paginator/reader interfaces only.
// No Create, Update, Delete, Associate, or Disassociate operation is present,
// and the domain-list reader (ListFirewallDomains) and rule-body reader
// (ListFirewallRules) are intentionally absent so the scanner cannot persist
// DNS Firewall domain contents or query log records.
type apiClient interface {
	awsr53r.ListResolverEndpointsAPIClient
	awsr53r.ListResolverEndpointIpAddressesAPIClient
	awsr53r.ListResolverRulesAPIClient
	awsr53r.ListResolverRuleAssociationsAPIClient
	awsr53r.ListFirewallRuleGroupsAPIClient
	awsr53r.ListFirewallDomainListsAPIClient
	awsr53r.ListFirewallRuleGroupAssociationsAPIClient
	awsr53r.ListResolverQueryLogConfigsAPIClient
	GetFirewallRuleGroup(
		context.Context,
		*awsr53r.GetFirewallRuleGroupInput,
		...func(*awsr53r.Options),
	) (*awsr53r.GetFirewallRuleGroupOutput, error)
	GetFirewallDomainList(
		context.Context,
		*awsr53r.GetFirewallDomainListInput,
		...func(*awsr53r.Options),
	) (*awsr53r.GetFirewallDomainListOutput, error)
	ListTagsForResource(
		context.Context,
		*awsr53r.ListTagsForResourceInput,
		...func(*awsr53r.Options),
	) (*awsr53r.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Route 53 Resolver pagination into scanner-owned
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Route 53 Resolver SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsr53r.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListResolverEndpoints returns all resolver endpoints with their subnet
// placements derived from each endpoint's IP addresses. The IP strings
// themselves are never carried.
func (c *Client) ListResolverEndpoints(ctx context.Context) ([]r53rservice.ResolverEndpoint, error) {
	paginator := awsr53r.NewListResolverEndpointsPaginator(c.client, &awsr53r.ListResolverEndpointsInput{})
	var endpoints []r53rservice.ResolverEndpoint
	for paginator.HasMorePages() {
		var page *awsr53r.ListResolverEndpointsOutput
		err := c.recordAPICall(ctx, "ListResolverEndpoints", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, endpoint := range page.ResolverEndpoints {
			id := aws.ToString(endpoint.Id)
			subnetIDs, err := c.listEndpointSubnets(ctx, id)
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(endpoint.Arn))
			if err != nil {
				return nil, err
			}
			endpoints = append(endpoints, mapResolverEndpoint(endpoint, subnetIDs, tags))
		}
	}
	return endpoints, nil
}

func (c *Client) listEndpointSubnets(ctx context.Context, endpointID string) ([]string, error) {
	if strings.TrimSpace(endpointID) == "" {
		return nil, nil
	}
	paginator := awsr53r.NewListResolverEndpointIpAddressesPaginator(
		c.client,
		&awsr53r.ListResolverEndpointIpAddressesInput{ResolverEndpointId: aws.String(endpointID)},
	)
	var subnetIDs []string
	for paginator.HasMorePages() {
		var page *awsr53r.ListResolverEndpointIpAddressesOutput
		err := c.recordAPICall(ctx, "ListResolverEndpointIpAddresses", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, address := range page.IpAddresses {
			if subnetID := strings.TrimSpace(aws.ToString(address.SubnetId)); subnetID != "" {
				subnetIDs = append(subnetIDs, subnetID)
			}
		}
	}
	return subnetIDs, nil
}

// ListResolverRules returns all resolver rules visible to the credentials.
func (c *Client) ListResolverRules(ctx context.Context) ([]r53rservice.ResolverRule, error) {
	paginator := awsr53r.NewListResolverRulesPaginator(c.client, &awsr53r.ListResolverRulesInput{})
	var rules []r53rservice.ResolverRule
	for paginator.HasMorePages() {
		var page *awsr53r.ListResolverRulesOutput
		err := c.recordAPICall(ctx, "ListResolverRules", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, rule := range page.ResolverRules {
			tags, err := c.listTags(ctx, aws.ToString(rule.Arn))
			if err != nil {
				return nil, err
			}
			rules = append(rules, mapResolverRule(rule, tags))
		}
	}
	return rules, nil
}

// ListResolverRuleAssociations returns all resolver rule-to-VPC associations.
func (c *Client) ListResolverRuleAssociations(ctx context.Context) ([]r53rservice.ResolverRuleAssociation, error) {
	paginator := awsr53r.NewListResolverRuleAssociationsPaginator(
		c.client,
		&awsr53r.ListResolverRuleAssociationsInput{},
	)
	var associations []r53rservice.ResolverRuleAssociation
	for paginator.HasMorePages() {
		var page *awsr53r.ListResolverRuleAssociationsOutput
		err := c.recordAPICall(ctx, "ListResolverRuleAssociations", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, association := range page.ResolverRuleAssociations {
			associations = append(associations, mapResolverRuleAssociation(association))
		}
	}
	return associations, nil
}

// ListFirewallRuleGroups returns all DNS Firewall rule groups with their
// AWS-reported rule counts. The rule bodies are never read.
func (c *Client) ListFirewallRuleGroups(ctx context.Context) ([]r53rservice.FirewallRuleGroup, error) {
	paginator := awsr53r.NewListFirewallRuleGroupsPaginator(c.client, &awsr53r.ListFirewallRuleGroupsInput{})
	var groups []r53rservice.FirewallRuleGroup
	for paginator.HasMorePages() {
		var page *awsr53r.ListFirewallRuleGroupsOutput
		err := c.recordAPICall(ctx, "ListFirewallRuleGroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, metadata := range page.FirewallRuleGroups {
			group, err := c.firewallRuleGroup(ctx, aws.ToString(metadata.Id))
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(metadata.Arn))
			if err != nil {
				return nil, err
			}
			group.Tags = tags
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func (c *Client) firewallRuleGroup(ctx context.Context, id string) (r53rservice.FirewallRuleGroup, error) {
	var output *awsr53r.GetFirewallRuleGroupOutput
	err := c.recordAPICall(ctx, "GetFirewallRuleGroup", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetFirewallRuleGroup(callCtx, &awsr53r.GetFirewallRuleGroupInput{
			FirewallRuleGroupId: aws.String(id),
		})
		return err
	})
	if err != nil {
		return r53rservice.FirewallRuleGroup{}, err
	}
	if output == nil || output.FirewallRuleGroup == nil {
		return r53rservice.FirewallRuleGroup{ID: id}, nil
	}
	return mapFirewallRuleGroup(*output.FirewallRuleGroup), nil
}

// ListFirewallDomainLists returns all DNS Firewall domain lists with their
// AWS-reported domain counts. The domain entries are never read.
func (c *Client) ListFirewallDomainLists(ctx context.Context) ([]r53rservice.FirewallDomainList, error) {
	paginator := awsr53r.NewListFirewallDomainListsPaginator(c.client, &awsr53r.ListFirewallDomainListsInput{})
	var lists []r53rservice.FirewallDomainList
	for paginator.HasMorePages() {
		var page *awsr53r.ListFirewallDomainListsOutput
		err := c.recordAPICall(ctx, "ListFirewallDomainLists", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, metadata := range page.FirewallDomainLists {
			list, err := c.firewallDomainList(ctx, aws.ToString(metadata.Id))
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(metadata.Arn))
			if err != nil {
				return nil, err
			}
			list.Tags = tags
			lists = append(lists, list)
		}
	}
	return lists, nil
}

func (c *Client) firewallDomainList(ctx context.Context, id string) (r53rservice.FirewallDomainList, error) {
	var output *awsr53r.GetFirewallDomainListOutput
	err := c.recordAPICall(ctx, "GetFirewallDomainList", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetFirewallDomainList(callCtx, &awsr53r.GetFirewallDomainListInput{
			FirewallDomainListId: aws.String(id),
		})
		return err
	})
	if err != nil {
		return r53rservice.FirewallDomainList{}, err
	}
	if output == nil || output.FirewallDomainList == nil {
		return r53rservice.FirewallDomainList{ID: id}, nil
	}
	return mapFirewallDomainList(*output.FirewallDomainList), nil
}

// ListFirewallRuleGroupAssociations returns all DNS Firewall rule group-to-VPC
// associations.
func (c *Client) ListFirewallRuleGroupAssociations(
	ctx context.Context,
) ([]r53rservice.FirewallRuleGroupAssociation, error) {
	paginator := awsr53r.NewListFirewallRuleGroupAssociationsPaginator(
		c.client,
		&awsr53r.ListFirewallRuleGroupAssociationsInput{},
	)
	var associations []r53rservice.FirewallRuleGroupAssociation
	for paginator.HasMorePages() {
		var page *awsr53r.ListFirewallRuleGroupAssociationsOutput
		err := c.recordAPICall(ctx, "ListFirewallRuleGroupAssociations", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, association := range page.FirewallRuleGroupAssociations {
			associations = append(associations, mapFirewallRuleGroupAssociation(association))
		}
	}
	return associations, nil
}

// ListQueryLogConfigs returns all Resolver query log configurations with their
// destination ARNs. Query log records are never read.
func (c *Client) ListQueryLogConfigs(ctx context.Context) ([]r53rservice.QueryLogConfig, error) {
	paginator := awsr53r.NewListResolverQueryLogConfigsPaginator(
		c.client,
		&awsr53r.ListResolverQueryLogConfigsInput{},
	)
	var configs []r53rservice.QueryLogConfig
	for paginator.HasMorePages() {
		var page *awsr53r.ListResolverQueryLogConfigsOutput
		err := c.recordAPICall(ctx, "ListResolverQueryLogConfigs", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, config := range page.ResolverQueryLogConfigs {
			tags, err := c.listTags(ctx, aws.ToString(config.Arn))
			if err != nil {
				return nil, err
			}
			configs = append(configs, mapQueryLogConfig(config, tags))
		}
	}
	return configs, nil
}

func (c *Client) listTags(ctx context.Context, arn string) (map[string]string, error) {
	if strings.TrimSpace(arn) == "" {
		return nil, nil
	}
	paginator := awsr53r.NewListTagsForResourcePaginator(
		c.client,
		&awsr53r.ListTagsForResourceInput{ResourceArn: aws.String(arn)},
	)
	tags := map[string]string{}
	for paginator.HasMorePages() {
		var page *awsr53r.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for key, value := range mapTags(page.Tags) {
			tags[key] = value
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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

var _ r53rservice.Client = (*Client)(nil)

var _ apiClient = (*awsr53r.Client)(nil)
