// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Direct Connect responses to the
// Direct Connect scanner contract.
//
// The adapter intentionally uses only DescribeXxx read operations:
// DescribeConnections, DescribeVirtualInterfaces, DescribeDirectConnectGateways,
// DescribeLags, and DescribeDirectConnectGatewayAssociations. Direct Connect
// ships no generated paginators, so each list follows the NextToken contract
// manually. No mutation method (Create/Delete/Update/Associate/Disassociate/
// Confirm/Allocate/Accept/Tag/Untag/Start/Stop) is named in the narrow
// apiClient interface, and a reflect-driven test pins that boundary against
// regressions.
//
// The adapter also never calls DescribeRouterConfiguration: that operation
// renders the BGP authentication key into the returned router configuration,
// so it is excluded by construction. The mapper drops VirtualInterface.AuthKey
// and BGP-peer auth keys, and drops Connection/LAG MacSecKeys (CKN and secret
// ARNs); only non-secret metadata reaches the scanner-owned records.
package awssdk
