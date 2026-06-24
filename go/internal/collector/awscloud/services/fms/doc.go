// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fms maps AWS Firewall Manager (FMS) policy metadata into AWS cloud
// collector facts.
//
// The scanner emits one aws_fms_policy resource per Firewall Manager policy,
// carrying policy identity, the governing security service type (WAF, WAFV2,
// SECURITY_GROUPS_COMMON, NETWORK_FIREWALL, SHIELD_ADVANCED, DNS_FIREWALL, and
// the other FMS security service types), the in-scope AWS resource type label,
// and the remediation flag. It emits one fms_policy_applies_to_account
// relationship per Organizations member account a policy is evaluated against,
// keyed on the bare 12-digit account id so the edge joins the
// aws_organizations_account node the organizations scanner publishes.
//
// FMS is an organization-wide control plane reachable only from the FMS
// administrator account, so one claim scans the administrator account's policy
// fleet. The scanner intentionally excludes policy rule payloads (the
// SecurityServicePolicyData managed service data document), account
// inclusion/exclusion maps, resource tag selectors, and every FMS mutation API.
package fms
