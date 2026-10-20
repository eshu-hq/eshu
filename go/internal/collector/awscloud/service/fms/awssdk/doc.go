// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Firewall Manager client into the
// metadata-only Firewall Manager scanner interface.
//
// The adapter reads ListPolicies for policy metadata and ListComplianceStatus
// for the per-policy member-account set. It intentionally excludes GetPolicy
// (which returns the policy rule payload, the SecurityServicePolicyData managed
// service data document), every FMS mutation API, and account
// inclusion/exclusion maps. ListPolicies already returns every policy metadata
// field the scanner records, so the rule payload is unreachable by
// construction.
package awssdk
