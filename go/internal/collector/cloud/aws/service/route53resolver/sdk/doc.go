// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Route 53 Resolver client to the
// route53resolver scanner contract.
//
// The package owns Route 53 Resolver pagination, per-resource count-derivation
// Get reads, SDK response mapping, AWS API telemetry, throttle detection, and
// pagination spans. It is metadata-only: the accepted apiClient surface
// excludes every Create, Update, Delete, Associate, and Disassociate operation
// by construction, and it omits the DNS Firewall domain reader
// (ListFirewallDomains) and rule reader (ListFirewallRules) so domain list
// contents and rule bodies can never be persisted. Reflective guard tests fail
// the build if any forbidden operation becomes reachable. Resolver endpoint IP
// address strings are read only to derive subnet placement and are never
// carried out of the adapter.
package awssdk
