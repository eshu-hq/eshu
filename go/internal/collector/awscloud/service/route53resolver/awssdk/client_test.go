// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsr53r "github.com/aws/aws-sdk-go-v2/service/route53resolver"
	awsr53rtypes "github.com/aws/aws-sdk-go-v2/service/route53resolver/types"
)

// fakeAPIClient is a metadata-only Route 53 Resolver read surface for adapter
// tests. It paginates each list operation across two pages and records the Get
// calls so the count-derivation path is exercised.
type fakeAPIClient struct {
	endpointPages    []*awsr53r.ListResolverEndpointsOutput
	ipAddressPages   map[string][]*awsr53r.ListResolverEndpointIpAddressesOutput
	rulePages        []*awsr53r.ListResolverRulesOutput
	ruleAssocPages   []*awsr53r.ListResolverRuleAssociationsOutput
	ruleGroupPages   []*awsr53r.ListFirewallRuleGroupsOutput
	domainListPages  []*awsr53r.ListFirewallDomainListsOutput
	fwAssocPages     []*awsr53r.ListFirewallRuleGroupAssociationsOutput
	queryLogPages    []*awsr53r.ListResolverQueryLogConfigsOutput
	ruleGroupGets    map[string]*awsr53r.GetFirewallRuleGroupOutput
	domainListGets   map[string]*awsr53r.GetFirewallDomainListOutput
	tags             map[string]*awsr53r.ListTagsForResourceOutput
	endpointCalls    int
	ruleCalls        int
	ruleAssocCalls   int
	ruleGroupCalls   int
	domainListCalls  int
	fwAssocCalls     int
	queryLogCalls    int
	ipAddressCalls   map[string]int
	ruleGroupGetIDs  []string
	domainListGetIDs []string
}

func nextPage[T any](pages []*T, call *int) *T {
	if *call >= len(pages) {
		var zero T
		return &zero
	}
	page := pages[*call]
	*call++
	return page
}

func (c *fakeAPIClient) ListResolverEndpoints(
	context.Context,
	*awsr53r.ListResolverEndpointsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListResolverEndpointsOutput, error) {
	return nextPage(c.endpointPages, &c.endpointCalls), nil
}

func (c *fakeAPIClient) ListResolverEndpointIpAddresses(
	_ context.Context,
	input *awsr53r.ListResolverEndpointIpAddressesInput,
	_ ...func(*awsr53r.Options),
) (*awsr53r.ListResolverEndpointIpAddressesOutput, error) {
	if c.ipAddressCalls == nil {
		c.ipAddressCalls = map[string]int{}
	}
	id := aws.ToString(input.ResolverEndpointId)
	pages := c.ipAddressPages[id]
	call := c.ipAddressCalls[id]
	if call >= len(pages) {
		return &awsr53r.ListResolverEndpointIpAddressesOutput{}, nil
	}
	c.ipAddressCalls[id] = call + 1
	return pages[call], nil
}

func (c *fakeAPIClient) ListResolverRules(
	context.Context,
	*awsr53r.ListResolverRulesInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListResolverRulesOutput, error) {
	return nextPage(c.rulePages, &c.ruleCalls), nil
}

func (c *fakeAPIClient) ListResolverRuleAssociations(
	context.Context,
	*awsr53r.ListResolverRuleAssociationsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListResolverRuleAssociationsOutput, error) {
	return nextPage(c.ruleAssocPages, &c.ruleAssocCalls), nil
}

func (c *fakeAPIClient) ListFirewallRuleGroups(
	context.Context,
	*awsr53r.ListFirewallRuleGroupsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListFirewallRuleGroupsOutput, error) {
	return nextPage(c.ruleGroupPages, &c.ruleGroupCalls), nil
}

func (c *fakeAPIClient) ListFirewallDomainLists(
	context.Context,
	*awsr53r.ListFirewallDomainListsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListFirewallDomainListsOutput, error) {
	return nextPage(c.domainListPages, &c.domainListCalls), nil
}

func (c *fakeAPIClient) ListFirewallRuleGroupAssociations(
	context.Context,
	*awsr53r.ListFirewallRuleGroupAssociationsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListFirewallRuleGroupAssociationsOutput, error) {
	return nextPage(c.fwAssocPages, &c.fwAssocCalls), nil
}

func (c *fakeAPIClient) ListResolverQueryLogConfigs(
	context.Context,
	*awsr53r.ListResolverQueryLogConfigsInput,
	...func(*awsr53r.Options),
) (*awsr53r.ListResolverQueryLogConfigsOutput, error) {
	return nextPage(c.queryLogPages, &c.queryLogCalls), nil
}

func (c *fakeAPIClient) GetFirewallRuleGroup(
	_ context.Context,
	input *awsr53r.GetFirewallRuleGroupInput,
	_ ...func(*awsr53r.Options),
) (*awsr53r.GetFirewallRuleGroupOutput, error) {
	id := aws.ToString(input.FirewallRuleGroupId)
	c.ruleGroupGetIDs = append(c.ruleGroupGetIDs, id)
	return c.ruleGroupGets[id], nil
}

func (c *fakeAPIClient) GetFirewallDomainList(
	_ context.Context,
	input *awsr53r.GetFirewallDomainListInput,
	_ ...func(*awsr53r.Options),
) (*awsr53r.GetFirewallDomainListOutput, error) {
	id := aws.ToString(input.FirewallDomainListId)
	c.domainListGetIDs = append(c.domainListGetIDs, id)
	return c.domainListGets[id], nil
}

func (c *fakeAPIClient) ListTagsForResource(
	_ context.Context,
	input *awsr53r.ListTagsForResourceInput,
	_ ...func(*awsr53r.Options),
) (*awsr53r.ListTagsForResourceOutput, error) {
	if c.tags == nil {
		return &awsr53r.ListTagsForResourceOutput{}, nil
	}
	if out, ok := c.tags[aws.ToString(input.ResourceArn)]; ok {
		return out, nil
	}
	return &awsr53r.ListTagsForResourceOutput{}, nil
}

func TestListResolverEndpointsPaginatesAndDerivesSubnets(t *testing.T) {
	api := &fakeAPIClient{
		endpointPages: []*awsr53r.ListResolverEndpointsOutput{
			{
				ResolverEndpoints: []awsr53rtypes.ResolverEndpoint{{
					Id:             aws.String("rslvr-out-1"),
					Arn:            aws.String("arn:endpoint:1"),
					Name:           aws.String("outbound"),
					Direction:      awsr53rtypes.ResolverEndpointDirectionOutbound,
					Status:         awsr53rtypes.ResolverEndpointStatusOperational,
					HostVPCId:      aws.String("vpc-aaa"),
					IpAddressCount: aws.Int32(2),
				}},
				NextToken: aws.String("next"),
			},
			{
				ResolverEndpoints: []awsr53rtypes.ResolverEndpoint{{
					Id:        aws.String("rslvr-in-1"),
					Arn:       aws.String("arn:endpoint:2"),
					Direction: awsr53rtypes.ResolverEndpointDirectionInbound,
				}},
			},
		},
		ipAddressPages: map[string][]*awsr53r.ListResolverEndpointIpAddressesOutput{
			"rslvr-out-1": {{
				IpAddresses: []awsr53rtypes.IpAddressResponse{
					{Ip: aws.String("10.0.0.10"), SubnetId: aws.String("subnet-aaa")},
					{Ip: aws.String("10.0.1.10"), SubnetId: aws.String("subnet-bbb")},
				},
			}},
		},
		tags: map[string]*awsr53r.ListTagsForResourceOutput{
			"arn:endpoint:1": {Tags: []awsr53rtypes.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}},
		},
	}
	client := &Client{client: api}

	endpoints, err := client.ListResolverEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListResolverEndpoints error: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("len(endpoints) = %d, want 2", len(endpoints))
	}
	if endpoints[0].Direction != "OUTBOUND" {
		t.Fatalf("direction = %q, want OUTBOUND", endpoints[0].Direction)
	}
	if endpoints[0].IPAddressCount != 2 {
		t.Fatalf("ip count = %d, want 2", endpoints[0].IPAddressCount)
	}
	if len(endpoints[0].SubnetIDs) != 2 {
		t.Fatalf("subnet ids = %v, want two", endpoints[0].SubnetIDs)
	}
	if endpoints[0].Tags["env"] != "prod" {
		t.Fatalf("tag env = %q, want prod", endpoints[0].Tags["env"])
	}
}

func TestListFirewallRuleGroupsDerivesRuleCountFromGet(t *testing.T) {
	api := &fakeAPIClient{
		ruleGroupPages: []*awsr53r.ListFirewallRuleGroupsOutput{{
			FirewallRuleGroups: []awsr53rtypes.FirewallRuleGroupMetadata{{
				Id:  aws.String("rslvr-frg-1"),
				Arn: aws.String("arn:frg:1"),
			}},
		}},
		ruleGroupGets: map[string]*awsr53r.GetFirewallRuleGroupOutput{
			"rslvr-frg-1": {FirewallRuleGroup: &awsr53rtypes.FirewallRuleGroup{
				Id:        aws.String("rslvr-frg-1"),
				Arn:       aws.String("arn:frg:1"),
				Name:      aws.String("blocklist"),
				RuleCount: aws.Int32(7),
				Status:    awsr53rtypes.FirewallRuleGroupStatusComplete,
			}},
		},
	}
	client := &Client{client: api}

	groups, err := client.ListFirewallRuleGroups(context.Background())
	if err != nil {
		t.Fatalf("ListFirewallRuleGroups error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if groups[0].RuleCount != 7 {
		t.Fatalf("rule count = %d, want 7", groups[0].RuleCount)
	}
	if len(api.ruleGroupGetIDs) != 1 || api.ruleGroupGetIDs[0] != "rslvr-frg-1" {
		t.Fatalf("rule group Get calls = %v, want [rslvr-frg-1]", api.ruleGroupGetIDs)
	}
}

func TestListFirewallDomainListsDerivesDomainCountFromGet(t *testing.T) {
	api := &fakeAPIClient{
		domainListPages: []*awsr53r.ListFirewallDomainListsOutput{{
			FirewallDomainLists: []awsr53rtypes.FirewallDomainListMetadata{{
				Id:  aws.String("rslvr-fdl-1"),
				Arn: aws.String("arn:fdl:1"),
			}},
		}},
		domainListGets: map[string]*awsr53r.GetFirewallDomainListOutput{
			"rslvr-fdl-1": {FirewallDomainList: &awsr53rtypes.FirewallDomainList{
				Id:          aws.String("rslvr-fdl-1"),
				Arn:         aws.String("arn:fdl:1"),
				Name:        aws.String("malware"),
				DomainCount: aws.Int32(2048),
				Status:      awsr53rtypes.FirewallDomainListStatusComplete,
			}},
		},
	}
	client := &Client{client: api}

	lists, err := client.ListFirewallDomainLists(context.Background())
	if err != nil {
		t.Fatalf("ListFirewallDomainLists error: %v", err)
	}
	if len(lists) != 1 {
		t.Fatalf("len(lists) = %d, want 1", len(lists))
	}
	if lists[0].DomainCount != 2048 {
		t.Fatalf("domain count = %d, want 2048", lists[0].DomainCount)
	}
	if len(api.domainListGetIDs) != 1 || api.domainListGetIDs[0] != "rslvr-fdl-1" {
		t.Fatalf("domain list Get calls = %v, want [rslvr-fdl-1]", api.domainListGetIDs)
	}
}

func TestListQueryLogConfigsCarriesDestination(t *testing.T) {
	api := &fakeAPIClient{
		queryLogPages: []*awsr53r.ListResolverQueryLogConfigsOutput{{
			ResolverQueryLogConfigs: []awsr53rtypes.ResolverQueryLogConfig{{
				Id:             aws.String("rslvr-qlc-1"),
				Arn:            aws.String("arn:qlc:1"),
				Name:           aws.String("logs"),
				DestinationArn: aws.String("arn:aws:s3:::dns-logs"),
				Status:         awsr53rtypes.ResolverQueryLogConfigStatusCreated,
			}},
		}},
	}
	client := &Client{client: api}

	configs, err := client.ListQueryLogConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListQueryLogConfigs error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].DestinationARN != "arn:aws:s3:::dns-logs" {
		t.Fatalf("destination = %q", configs[0].DestinationARN)
	}
}
