// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package transitgateway emits AWS Transit Gateway metadata fact evidence.
//
// The package owns the transit gateway hub surface: transit gateways, transit
// gateway route tables, transit gateway attachments (VPC, VPN, Direct Connect
// gateway, peering, and Connect), peering attachments, multicast domains, and
// policy tables. It pairs with services/vpc: the VPC scanner already references
// the transit gateway node from route-target and VPN-connection edges, and this
// scanner makes that node real. Attachment relationship edges cross back to the
// VPC-scanner-owned VPN connection and the EC2-owned VPC by identifier when the
// AWS API reports both sides directly.
//
// The scanner is metadata-only. It never reads transit gateway routes,
// multicast group memberships, or policy table rules. Cross-account peering
// attachments are emitted with the remote transit gateway identity exactly as
// AWS reports it; the scanner never resolves the remote account's identity,
// which is left to downstream org-context joins.
package transitgateway
