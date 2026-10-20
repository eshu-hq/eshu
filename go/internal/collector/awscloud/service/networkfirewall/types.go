// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkfirewall

import "context"

// Client is the Network Firewall read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned metadata
// records. The adapter must never request or return rule group rule sources
// (Suricata signature bodies), TLS inspection certificate bodies, or any
// firewall policy rule body, and it must never call a Network Firewall
// mutation API.
type Client interface {
	// ListFirewalls returns firewall metadata, the firewall policy ARN, VPC id,
	// and subnet mappings needed to project relationships. Implementations
	// resolve these from DescribeFirewall without persisting rule bodies.
	ListFirewalls(context.Context) ([]Firewall, error)
	// ListFirewallPolicies returns firewall policy metadata, default-action
	// names, and the rule group and TLS inspection configuration ARN references
	// needed to project relationships. The full policy rule body is never
	// returned.
	ListFirewallPolicies(context.Context) ([]FirewallPolicy, error)
	// ListRuleGroups returns rule group metadata including the type
	// (STATEFUL/STATELESS) and capacity. The rule source (Suricata signature
	// bodies) is never returned; implementations read metadata through
	// DescribeRuleGroupMetadata, which does not surface rule bodies.
	ListRuleGroups(context.Context) ([]RuleGroup, error)
	// ListTLSInspectionConfigurations returns TLS inspection configuration
	// metadata. Certificate bodies and TLS scope rule bodies are never returned.
	ListTLSInspectionConfigurations(context.Context) ([]TLSInspectionConfiguration, error)
}

// Firewall is the scanner-owned representation of one Network Firewall
// firewall. It carries identity, the associated firewall policy ARN, the VPC
// id, subnet mapping ids, protection flags, and reported status. Rule bodies
// are intentionally outside this contract.
type Firewall struct {
	ARN                            string
	ID                             string
	Name                           string
	Description                    string
	VPCID                          string
	FirewallPolicyARN              string
	SubnetIDs                      []string
	DeleteProtection               bool
	SubnetChangeProtection         bool
	FirewallPolicyChangeProtection bool
	Status                         string
	ConfigurationSyncState         string
	NumberOfAssociations           int32
	Tags                           map[string]string
}

// FirewallPolicy is the scanner-owned representation of one Network Firewall
// firewall policy. It carries identity, the default-action names, and the rule
// group and TLS inspection configuration ARN references. The full policy rule
// body is intentionally outside this contract; only action names and reference
// ARNs are persisted.
type FirewallPolicy struct {
	ARN                             string
	ID                              string
	Name                            string
	Description                     string
	Status                          string
	StatelessDefaultActions         []string
	StatelessFragmentDefaultActions []string
	StatefulDefaultActions          []string
	RuleGroupARNs                   []string
	TLSInspectionConfigurationARN   string
	NumberOfAssociations            int32
	ConsumedStatefulRuleCapacity    int32
	ConsumedStatelessRuleCapacity   int32
	Tags                            map[string]string
}

// RuleGroup is the scanner-owned representation of one Network Firewall rule
// group. It carries identity, the rule group type (STATEFUL or STATELESS), and
// the configured capacity. The rule source (Suricata signature bodies for
// stateful groups, stateless rule definitions for stateless groups) is
// intentionally outside this contract and is never read, because it is
// threat-detection intelligence.
type RuleGroup struct {
	ARN                  string
	ID                   string
	Name                 string
	Description          string
	Type                 string
	Capacity             int32
	NumberOfAssociations int32
	Tags                 map[string]string
}

// TLSInspectionConfiguration is the scanner-owned representation of one Network
// Firewall TLS inspection configuration. It carries identity and aggregate
// metadata only. Certificate bodies, certificate authority ARNs, and TLS
// inspection scope rule bodies are intentionally outside this contract.
type TLSInspectionConfiguration struct {
	ARN                  string
	ID                   string
	Name                 string
	Description          string
	Status               string
	NumberOfAssociations int32
	Tags                 map[string]string
}
