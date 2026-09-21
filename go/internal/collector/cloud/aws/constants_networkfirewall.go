// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceNetworkFirewall identifies the regional AWS Network Firewall
	// metadata scan slice. One claim scans firewalls, firewall policies, rule
	// groups, and TLS inspection configurations for its boundary account and
	// region. Rule group rule sources (Suricata signature bodies) are never
	// read or persisted because they are threat-detection intelligence.
	ServiceNetworkFirewall = "networkfirewall"
)

const (
	// ResourceTypeNetworkFirewallFirewall identifies a Network Firewall firewall
	// metadata resource.
	ResourceTypeNetworkFirewallFirewall = "aws_networkfirewall_firewall"
	// ResourceTypeNetworkFirewallPolicy identifies a Network Firewall firewall
	// policy metadata resource. The resource carries identity and default-action
	// names only; the full policy rule body is never persisted.
	ResourceTypeNetworkFirewallPolicy = "aws_networkfirewall_firewall_policy"
	// ResourceTypeNetworkFirewallRuleGroup identifies a Network Firewall rule
	// group metadata resource. The resource carries identity, type
	// (STATEFUL/STATELESS), and capacity only; the rule source (Suricata
	// signature bodies) is never read or persisted because it is threat
	// intelligence.
	ResourceTypeNetworkFirewallRuleGroup = "aws_networkfirewall_rule_group"
	// ResourceTypeNetworkFirewallTLSInspectionConfiguration identifies a Network
	// Firewall TLS inspection configuration metadata resource. The resource
	// carries identity and aggregate metadata only; certificate bodies and TLS
	// inspection scope rule bodies are never persisted.
	ResourceTypeNetworkFirewallTLSInspectionConfiguration = "aws_networkfirewall_tls_inspection_configuration"
)

const (
	// RelationshipNetworkFirewallFirewallInVPC records a firewall's reported VPC
	// placement. The target is the bare VPC id and resolves to an EC2-owned VPC.
	RelationshipNetworkFirewallFirewallInVPC = "networkfirewall_firewall_in_vpc"
	// RelationshipNetworkFirewallFirewallUsesSubnet records a firewall's reported
	// subnet mapping. The target is the bare subnet id and resolves to an
	// EC2-owned subnet.
	RelationshipNetworkFirewallFirewallUsesSubnet = "networkfirewall_firewall_uses_subnet"
	// RelationshipNetworkFirewallFirewallUsesPolicy records a firewall's reference
	// to its firewall policy by ARN.
	RelationshipNetworkFirewallFirewallUsesPolicy = "networkfirewall_firewall_uses_policy"
	// RelationshipNetworkFirewallPolicyUsesRuleGroup records a firewall policy's
	// reference to a stateful or stateless rule group by ARN.
	RelationshipNetworkFirewallPolicyUsesRuleGroup = "networkfirewall_policy_uses_rule_group"
	// RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration records a
	// firewall policy's reference to its TLS inspection configuration by ARN.
	RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration = "networkfirewall_policy_uses_tls_inspection_configuration"
)
