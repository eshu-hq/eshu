// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Network Manager client into the
// metadata-only Network Manager scanner interface.
//
// The adapter uses DescribeGlobalNetworks, GetSites, GetDevices, GetLinks,
// GetConnections, GetLinkAssociations, GetTransitGatewayRegistrations,
// ListCoreNetworks, and GetCoreNetwork to read Network Manager control-plane
// metadata. Network Manager is global, so NewClient pins the SDK client to the
// partition's control-plane region (us-west-2 commercial, us-gov-west-1
// GovCloud, cn-north-1 China) regardless of the claim region. The adapter
// intentionally excludes every Create/Update/Delete mutation, every
// Register/Deregister and Associate/Disassociate call, route-analysis starts,
// and tag writes, so it cannot mutate Network Manager state.
package awssdk
