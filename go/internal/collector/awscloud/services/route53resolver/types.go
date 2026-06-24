// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver

import "context"

// Client is the AWS Route 53 Resolver read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types. The
// surface is metadata-only: it exposes no Create, Update, Delete, Associate, or
// Disassociate operation, and it never reads DNS Firewall domain list contents
// (ListFirewallDomains) or query log records.
//
// Firewall rule-group and domain-list counts come from the per-resource Get
// reads, which return only metadata and an aggregate count. The rule-body
// readers (ListFirewallRules) and domain readers (ListFirewallDomains) are
// intentionally absent from this interface.
type Client interface {
	ListResolverEndpoints(context.Context) ([]ResolverEndpoint, error)
	ListResolverRules(context.Context) ([]ResolverRule, error)
	ListResolverRuleAssociations(context.Context) ([]ResolverRuleAssociation, error)
	ListFirewallRuleGroups(context.Context) ([]FirewallRuleGroup, error)
	ListFirewallDomainLists(context.Context) ([]FirewallDomainList, error)
	ListFirewallRuleGroupAssociations(context.Context) ([]FirewallRuleGroupAssociation, error)
	ListQueryLogConfigs(context.Context) ([]QueryLogConfig, error)
}

// ResolverEndpoint is the scanner-owned representation of a Route 53 Resolver
// inbound or outbound endpoint. SubnetIDs are derived from the endpoint IP
// addresses; the IP strings themselves are never carried, only the subnet
// placement and the AWS-reported IP count.
type ResolverEndpoint struct {
	ID             string
	ARN            string
	Name           string
	Direction      string
	Status         string
	IPAddressCount int32
	HostVPCID      string
	SubnetIDs      []string
	Tags           map[string]string
}

// ResolverRule is the scanner-owned representation of a Route 53 Resolver rule.
// DomainName is the reported match domain. RuleType is FORWARD, SYSTEM, or
// RECURSIVE. Forwarded target query data is not carried beyond the domain name
// and rule shape.
type ResolverRule struct {
	ID                 string
	ARN                string
	Name               string
	DomainName         string
	RuleType           string
	Status             string
	ShareStatus        string
	ResolverEndpointID string
	Tags               map[string]string
}

// ResolverRuleAssociation is the scanner-owned representation of a resolver
// rule-to-VPC association.
type ResolverRuleAssociation struct {
	ID             string
	Name           string
	ResolverRuleID string
	VPCID          string
	Status         string
}

// FirewallRuleGroup is the scanner-owned representation of a DNS Firewall rule
// group. RuleCount is the AWS-reported count of rules in the group; the rule
// bodies are never read.
type FirewallRuleGroup struct {
	ID          string
	ARN         string
	Name        string
	RuleCount   int32
	Status      string
	ShareStatus string
	OwnerID     string
	Tags        map[string]string
}

// FirewallDomainList is the scanner-owned representation of a DNS Firewall
// domain list. DomainCount is the AWS-reported count of domains in the list;
// the domain entries themselves are never read.
type FirewallDomainList struct {
	ID               string
	ARN              string
	Name             string
	DomainCount      int32
	Status           string
	ManagedOwnerName string
	Tags             map[string]string
}

// FirewallRuleGroupAssociation is the scanner-owned representation of a DNS
// Firewall rule group-to-VPC association.
type FirewallRuleGroupAssociation struct {
	ID                  string
	ARN                 string
	Name                string
	FirewallRuleGroupID string
	VPCID               string
	Priority            int32
	MutationProtection  string
	Status              string
}

// QueryLogConfig is the scanner-owned representation of a Resolver query log
// configuration. Only the destination ARN and identity metadata are carried;
// query log records are never read.
type QueryLogConfig struct {
	ID             string
	ARN            string
	Name           string
	DestinationARN string
	Status         string
	ShareStatus    string
	OwnerID        string
	Tags           map[string]string
}
