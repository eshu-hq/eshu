// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 EC2 responses to the Transit Gateway
// scanner contract.
//
// The adapter intentionally uses only DescribeXxx read paginator operations:
// DescribeTransitGateways, DescribeTransitGatewayRouteTables,
// DescribeTransitGatewayAttachments, DescribeTransitGatewayPeeringAttachments,
// DescribeTransitGatewayMulticastDomains, and DescribeTransitGatewayPolicyTables.
// No mutation method (Create/Delete/Modify/Associate/Disassociate/Enable/
// Disable/Accept/Reject/Replace/Register/Deregister) is embedded into the narrow
// apiClient interface, and a reflect-driven test pins that boundary against
// regressions. The adapter also never reads transit gateway routes or policy
// table entries.
package awssdk
