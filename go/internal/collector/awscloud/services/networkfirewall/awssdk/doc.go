// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Network Firewall calls into
// scanner-owned metadata.
//
// The adapter calls only read operations: ListFirewalls, DescribeFirewall,
// ListFirewallPolicies, DescribeFirewallPolicy, ListRuleGroups,
// DescribeRuleGroupMetadata, ListTLSInspectionConfigurations,
// DescribeTLSInspectionConfiguration, and ListTagsForResource. Its apiClient
// interface exposes no mutation method, and a reflection gate fails the build
// path if one is added. Rule group metadata is read through
// DescribeRuleGroupMetadata, which never returns the rule source (Suricata
// signature bodies); DescribeRuleGroup is excluded by construction because its
// output carries the rule source. The firewall policy read keeps only
// default-action names and rule group / TLS inspection configuration reference
// ARNs, not the full policy rule body, and the TLS inspection read keeps only
// response metadata, not certificate bodies. Network Firewall is regional, so
// the adapter scans the boundary region directly.
package awssdk
