// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53resolver"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceRoute53Resolver,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
	}
}

type fakeClient struct {
	endpoints         []route53resolver.ResolverEndpoint
	rules             []route53resolver.ResolverRule
	ruleAssociations  []route53resolver.ResolverRuleAssociation
	ruleGroups        []route53resolver.FirewallRuleGroup
	domainLists       []route53resolver.FirewallDomainList
	groupAssociations []route53resolver.FirewallRuleGroupAssociation
	queryLogConfigs   []route53resolver.QueryLogConfig
	err               error
}

func (c fakeClient) ListResolverEndpoints(context.Context) ([]route53resolver.ResolverEndpoint, error) {
	return c.endpoints, c.err
}

func (c fakeClient) ListResolverRules(context.Context) ([]route53resolver.ResolverRule, error) {
	return c.rules, c.err
}

func (c fakeClient) ListResolverRuleAssociations(context.Context) ([]route53resolver.ResolverRuleAssociation, error) {
	return c.ruleAssociations, c.err
}

func (c fakeClient) ListFirewallRuleGroups(context.Context) ([]route53resolver.FirewallRuleGroup, error) {
	return c.ruleGroups, c.err
}

func (c fakeClient) ListFirewallDomainLists(context.Context) ([]route53resolver.FirewallDomainList, error) {
	return c.domainLists, c.err
}

func (c fakeClient) ListFirewallRuleGroupAssociations(
	context.Context,
) ([]route53resolver.FirewallRuleGroupAssociation, error) {
	return c.groupAssociations, c.err
}

func (c fakeClient) ListQueryLogConfigs(context.Context) ([]route53resolver.QueryLogConfig, error) {
	return c.queryLogConfigs, c.err
}

func sampleClient() fakeClient {
	return fakeClient{
		endpoints: []route53resolver.ResolverEndpoint{{
			ID:             "rslvr-out-1",
			ARN:            "arn:aws:route53resolver:us-east-1:123456789012:resolver-endpoint/rslvr-out-1",
			Name:           "outbound",
			Direction:      "OUTBOUND",
			Status:         "OPERATIONAL",
			IPAddressCount: 2,
			HostVPCID:      "vpc-aaa",
			SubnetIDs:      []string{"subnet-aaa", "subnet-bbb", "subnet-aaa"},
		}},
		rules: []route53resolver.ResolverRule{{
			ID:                 "rslvr-rr-1",
			ARN:                "arn:aws:route53resolver:us-east-1:123456789012:resolver-rule/rslvr-rr-1",
			Name:               "forward-internal",
			DomainName:         "internal.example.com.",
			RuleType:           "FORWARD",
			Status:             "COMPLETE",
			ResolverEndpointID: "rslvr-out-1",
		}},
		ruleAssociations: []route53resolver.ResolverRuleAssociation{{
			ID:             "rslvr-rrassoc-1",
			Name:           "assoc",
			ResolverRuleID: "rslvr-rr-1",
			VPCID:          "vpc-bbb",
			Status:         "COMPLETE",
		}},
		ruleGroups: []route53resolver.FirewallRuleGroup{{
			ID:        "rslvr-frg-1",
			ARN:       "arn:aws:route53resolver:us-east-1:123456789012:firewall-rule-group/rslvr-frg-1",
			Name:      "blocklist-group",
			RuleCount: 5,
			Status:    "COMPLETE",
		}},
		domainLists: []route53resolver.FirewallDomainList{{
			ID:          "rslvr-fdl-1",
			ARN:         "arn:aws:route53resolver:us-east-1:123456789012:firewall-domain-list/rslvr-fdl-1",
			Name:        "malware-domains",
			DomainCount: 4096,
			Status:      "COMPLETE",
		}},
		groupAssociations: []route53resolver.FirewallRuleGroupAssociation{{
			ID:                  "rslvr-frgassoc-1",
			ARN:                 "arn:aws:route53resolver:us-east-1:123456789012:firewall-rule-group-association/rslvr-frgassoc-1",
			Name:                "fw-assoc",
			FirewallRuleGroupID: "rslvr-frg-1",
			VPCID:               "vpc-ccc",
			Priority:            101,
			Status:              "COMPLETE",
		}},
		queryLogConfigs: []route53resolver.QueryLogConfig{{
			ID:             "rslvr-qlc-1",
			ARN:            "arn:aws:route53resolver:us-east-1:123456789012:resolver-query-log-config/rslvr-qlc-1",
			Name:           "query-logs",
			DestinationARN: "arn:aws:s3:::dns-query-logs",
			Status:         "CREATED",
		}},
	}
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func TestScannerRequiresClient(t *testing.T) {
	var scanner route53resolver.Scanner
	if _, err := scanner.Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerRejectsForeignServiceKind(t *testing.T) {
	scanner := route53resolver.Scanner{Client: fakeClient{}}
	boundary := testBoundary()
	boundary.ServiceKind = "route53"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerSurfacesClientError(t *testing.T) {
	wrapped := errors.New("boom")
	scanner := route53resolver.Scanner{Client: fakeClient{err: wrapped}}
	_, err := scanner.Scan(context.Background(), testBoundary())
	if !errors.Is(err, wrapped) {
		t.Fatalf("Scan() error = %v, want wrapped client error", err)
	}
}

func TestScannerEmitsAllResourceKinds(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, resourceType := range []string{
		awscloud.ResourceTypeRoute53ResolverEndpoint,
		awscloud.ResourceTypeRoute53ResolverRule,
		awscloud.ResourceTypeRoute53ResolverRuleAssociation,
		awscloud.ResourceTypeRoute53ResolverFirewallRuleGroup,
		awscloud.ResourceTypeRoute53ResolverFirewallDomainList,
		awscloud.ResourceTypeRoute53ResolverFirewallRuleGroupAssociation,
		awscloud.ResourceTypeRoute53ResolverQueryLogConfig,
	} {
		if got := resourcesByType(t, envelopes, resourceType); len(got) != 1 {
			t.Fatalf("resource %q count = %d, want 1", resourceType, len(got))
		}
	}
}

func TestRelationshipsHaveTargetTypeAndJoinKeys(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	cases := []struct {
		relationshipType string
		targetType       string
		targetResourceID string
	}{
		{
			relationshipType: awscloud.RelationshipRoute53ResolverEndpointInVPC,
			targetType:       awscloud.ResourceTypeEC2VPC,
			targetResourceID: "vpc-aaa",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverEndpointUsesSubnet,
			targetType:       awscloud.ResourceTypeEC2Subnet,
			targetResourceID: "subnet-aaa",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverRuleUsesEndpoint,
			targetType:       awscloud.ResourceTypeRoute53ResolverEndpoint,
			targetResourceID: "rslvr-out-1",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverRuleAssociationTargetsVPC,
			targetType:       awscloud.ResourceTypeEC2VPC,
			targetResourceID: "vpc-bbb",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverRuleAssociationUsesRule,
			targetType:       awscloud.ResourceTypeRoute53ResolverRule,
			targetResourceID: "rslvr-rr-1",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC,
			targetType:       awscloud.ResourceTypeEC2VPC,
			targetResourceID: "vpc-ccc",
		},
		{
			relationshipType: awscloud.RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup,
			targetType:       awscloud.ResourceTypeRoute53ResolverFirewallRuleGroup,
			targetResourceID: "rslvr-frg-1",
		},
	}
	for _, tc := range cases {
		relationships := relationshipsByType(t, envelopes, tc.relationshipType)
		if len(relationships) == 0 {
			t.Fatalf("relationship %q not emitted", tc.relationshipType)
		}
		match := false
		for _, relationship := range relationships {
			if relationship["target_type"] == "" {
				t.Fatalf("relationship %q has empty target_type", tc.relationshipType)
			}
			if relationship["target_resource_id"] == tc.targetResourceID {
				if relationship["target_type"] != tc.targetType {
					t.Fatalf("relationship %q target_type = %q, want %q", tc.relationshipType, relationship["target_type"], tc.targetType)
				}
				match = true
			}
		}
		if !match {
			t.Fatalf("relationship %q missing target_resource_id %q", tc.relationshipType, tc.targetResourceID)
		}
	}
}

// TestEndpointSubnetRelationshipsDeduplicate confirms repeated subnet IDs from
// multiple endpoint IP addresses produce one edge per subnet.
func TestEndpointSubnetRelationshipsDeduplicate(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	relationships := relationshipsByType(t, envelopes, awscloud.RelationshipRoute53ResolverEndpointUsesSubnet)
	if len(relationships) != 2 {
		t.Fatalf("endpoint subnet edges = %d, want 2 (subnet-aaa deduplicated)", len(relationships))
	}
}

// TestFirewallDomainListCarriesCountNotContents is the privacy acceptance gate
// from issue #838: the domain list fact must carry an aggregate domain count
// and never any domain entry.
func TestFirewallDomainListCarriesCountNotContents(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	lists := resourcesByType(t, envelopes, awscloud.ResourceTypeRoute53ResolverFirewallDomainList)
	if len(lists) != 1 {
		t.Fatalf("domain list count = %d, want 1", len(lists))
	}
	attrs, ok := lists[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("domain list attributes missing")
	}
	if attrs["domain_count"] != int32(4096) {
		t.Fatalf("domain_count = %v, want 4096", attrs["domain_count"])
	}
	for _, forbidden := range []string{"domains", "domain_list", "domain", "entries"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("domain list attribute %q must not be present", forbidden)
		}
	}
}

// TestFirewallRuleGroupCarriesCountNotRules confirms the rule group fact carries
// the AWS-reported rule count and never any rule body.
func TestFirewallRuleGroupCarriesCountNotRules(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	groups := resourcesByType(t, envelopes, awscloud.ResourceTypeRoute53ResolverFirewallRuleGroup)
	if len(groups) != 1 {
		t.Fatalf("rule group count = %d, want 1", len(groups))
	}
	attrs, ok := groups[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("rule group attributes missing")
	}
	if attrs["rule_count"] != int32(5) {
		t.Fatalf("rule_count = %v, want 5", attrs["rule_count"])
	}
	for _, forbidden := range []string{"rules", "rule_list", "firewall_rules"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("rule group attribute %q must not be present", forbidden)
		}
	}
}

// TestQueryLogConfigCarriesDestinationOnly confirms the query log config fact
// carries the destination ARN and never any query log record.
func TestQueryLogConfigCarriesDestinationOnly(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	configs := resourcesByType(t, envelopes, awscloud.ResourceTypeRoute53ResolverQueryLogConfig)
	if len(configs) != 1 {
		t.Fatalf("query log config count = %d, want 1", len(configs))
	}
	attrs, ok := configs[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("query log config attributes missing")
	}
	if attrs["destination_arn"] != "arn:aws:s3:::dns-query-logs" {
		t.Fatalf("destination_arn = %v", attrs["destination_arn"])
	}
	for _, forbidden := range []string{"records", "query_log_records", "queries"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("query log config attribute %q must not be present", forbidden)
		}
	}
}

// TestResolverRulePreservesTypeAndDomain confirms FORWARD/SYSTEM/RECURSIVE rule
// type and the reported domain name are carried without forwarded query data.
func TestResolverRulePreservesTypeAndDomain(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	rules := resourcesByType(t, envelopes, awscloud.ResourceTypeRoute53ResolverRule)
	if len(rules) != 1 {
		t.Fatalf("rule count = %d, want 1", len(rules))
	}
	attrs, ok := rules[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("rule attributes missing")
	}
	if attrs["rule_type"] != "FORWARD" {
		t.Fatalf("rule_type = %v, want FORWARD", attrs["rule_type"])
	}
	if attrs["domain_name"] != "internal.example.com." {
		t.Fatalf("domain_name = %v", attrs["domain_name"])
	}
	for _, forbidden := range []string{"target_ips", "target_addresses", "targets"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("rule attribute %q must not be present (forwarded query data)", forbidden)
		}
	}
}

// TestResolverEndpointCarriesIPCountNotAddresses confirms the endpoint fact
// carries the IP count and subnet count but never the IP address strings.
func TestResolverEndpointCarriesIPCountNotAddresses(t *testing.T) {
	scanner := route53resolver.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	endpoints := resourcesByType(t, envelopes, awscloud.ResourceTypeRoute53ResolverEndpoint)
	if len(endpoints) != 1 {
		t.Fatalf("endpoint count = %d, want 1", len(endpoints))
	}
	attrs, ok := endpoints[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("endpoint attributes missing")
	}
	if attrs["ip_address_count"] != int32(2) {
		t.Fatalf("ip_address_count = %v, want 2", attrs["ip_address_count"])
	}
	// subnet_count must reflect the deduplicated subnet set so it matches the
	// emitted endpoint->subnet edge cardinality. The fixture supplies three
	// raw IP-derived subnet IDs ("subnet-aaa", "subnet-bbb", "subnet-aaa") that
	// resolve to two unique subnets.
	if attrs["subnet_count"] != 2 {
		t.Fatalf("subnet_count = %v, want 2 (unique subnets, not raw IP-derived count)", attrs["subnet_count"])
	}
	if attrs["direction"] != "OUTBOUND" {
		t.Fatalf("direction = %v, want OUTBOUND", attrs["direction"])
	}
	for _, forbidden := range []string{"ip_addresses", "ips", "addresses"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("endpoint attribute %q must not be present", forbidden)
		}
	}
}
