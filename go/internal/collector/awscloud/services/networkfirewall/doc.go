// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package networkfirewall maps AWS Network Firewall metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level normalization only. It never calls the AWS
// SDK directly and never persists rule group rule sources (Suricata signature
// bodies), firewall policy rule bodies, or TLS inspection certificate bodies.
// SDK adapters provide Firewall, FirewallPolicy, RuleGroup, and
// TLSInspectionConfiguration values that carry identity, aggregate metadata,
// default-action names, and reference ARNs; Scanner emits aws_resource facts
// plus firewall-to-VPC, firewall-to-subnet, firewall-to-policy,
// policy-to-rule-group, and policy-to-TLS-inspection-configuration relationship
// evidence.
package networkfirewall
