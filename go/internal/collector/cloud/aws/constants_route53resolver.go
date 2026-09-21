// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceRoute53Resolver identifies the regional Amazon Route 53 Resolver
	// metadata scan slice. The slice covers resolver endpoints, resolver rules
	// and rule associations, DNS Firewall rule groups and domain lists
	// (metadata and counts only), firewall rule group associations, and query
	// log configurations (destination only). It never reads DNS Firewall domain
	// list contents or query log records, and it never mutates a resource.
	//
	// This is distinct from ServiceRoute53, which owns hosted zones and DNS
	// records. Route 53 Resolver is a regional service, so the boundary carries
	// the real claim region rather than aws-global.
	ServiceRoute53Resolver = "route53resolver"
)

const (
	// ResourceTypeRoute53ResolverEndpoint identifies a Route 53 Resolver inbound
	// or outbound endpoint. The direction attribute disambiguates the variant.
	ResourceTypeRoute53ResolverEndpoint = "aws_route53resolver_endpoint"
	// ResourceTypeRoute53ResolverRule identifies a Route 53 Resolver rule. The
	// rule_type attribute carries FORWARD, SYSTEM, or RECURSIVE. The scanner
	// never persists forwarded target query data beyond the reported domain
	// name and rule shape.
	ResourceTypeRoute53ResolverRule = "aws_route53resolver_rule"
	// ResourceTypeRoute53ResolverRuleAssociation identifies a resolver
	// rule-to-VPC association reported by AWS.
	ResourceTypeRoute53ResolverRuleAssociation = "aws_route53resolver_rule_association"
	// ResourceTypeRoute53ResolverFirewallRuleGroup identifies a DNS Firewall
	// rule group. The resource carries the AWS-reported rule count only; the
	// scanner never reads the rule bodies via ListFirewallRules.
	ResourceTypeRoute53ResolverFirewallRuleGroup = "aws_route53resolver_firewall_rule_group"
	// ResourceTypeRoute53ResolverFirewallDomainList identifies a DNS Firewall
	// domain list. The resource carries the AWS-reported domain count only; the
	// scanner never reads the domain entries via ListFirewallDomains.
	ResourceTypeRoute53ResolverFirewallDomainList = "aws_route53resolver_firewall_domain_list"
	// ResourceTypeRoute53ResolverFirewallRuleGroupAssociation identifies a DNS
	// Firewall rule group-to-VPC association reported by AWS.
	ResourceTypeRoute53ResolverFirewallRuleGroupAssociation = "aws_route53resolver_firewall_rule_group_association" // #nosec G101 -- AWS resource-type identifier constant, not a credential
	// ResourceTypeRoute53ResolverQueryLogConfig identifies a Resolver query log
	// configuration. The resource carries the reported destination ARN only;
	// the scanner never reads query log records.
	ResourceTypeRoute53ResolverQueryLogConfig = "aws_route53resolver_query_log_config" // #nosec G101 -- resource-type identifier, not a credential
)

const (
	// RelationshipRoute53ResolverEndpointInVPC records a resolver endpoint's
	// reported host VPC placement.
	RelationshipRoute53ResolverEndpointInVPC = "route53resolver_endpoint_in_vpc"
	// RelationshipRoute53ResolverEndpointUsesSubnet records a subnet reported by
	// one of a resolver endpoint's IP addresses.
	RelationshipRoute53ResolverEndpointUsesSubnet = "route53resolver_endpoint_uses_subnet"
	// RelationshipRoute53ResolverRuleUsesEndpoint records the outbound resolver
	// endpoint a FORWARD rule routes queries through.
	RelationshipRoute53ResolverRuleUsesEndpoint = "route53resolver_rule_uses_endpoint"
	// RelationshipRoute53ResolverRuleAssociationTargetsVPC records the VPC a
	// resolver rule association binds the rule to.
	RelationshipRoute53ResolverRuleAssociationTargetsVPC = "route53resolver_rule_association_targets_vpc"
	// RelationshipRoute53ResolverRuleAssociationUsesRule records the resolver
	// rule a rule association binds to a VPC.
	RelationshipRoute53ResolverRuleAssociationUsesRule = "route53resolver_rule_association_uses_rule"
	// RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC records
	// the VPC a DNS Firewall rule group association binds the rule group to.
	RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC = "route53resolver_firewall_rule_group_association_targets_vpc"
	// RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup
	// records the DNS Firewall rule group a rule group association binds to a
	// VPC.
	RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup = "route53resolver_firewall_rule_group_association_uses_rule_group"
)
