// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package directconnect maps AWS Direct Connect metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level Direct Connect normalization only. It never
// calls the AWS SDK directly and never calls a Direct Connect mutation API.
// SDK adapters provide already-resolved connection, virtual interface, Direct
// Connect gateway, LAG, and gateway-association records, and Scanner emits
// aws_resource facts plus aws_relationship facts for the edges Direct Connect
// reports directly:
//
//   - connection -> LAG (the physical port's link aggregation group)
//   - virtual interface -> Direct Connect gateway
//   - virtual interface -> connection (the port the interface runs over)
//   - Direct Connect gateway -> transit gateway (gateway association)
//   - Direct Connect gateway -> virtual private gateway (gateway association)
//
// The Direct Connect gateway resource uses resource_type
// aws_direct_connect_gateway and the bare gateway ID as resource_id. That
// matches the transit_gateway_attachment_to_direct_connect_gateway edge the
// transitgateway scanner already emits, so that previously dangling edge
// resolves once this scanner runs. The gateway-to-transit-gateway and
// gateway-to-virtual-private-gateway edges target the transitgateway-owned
// aws_ec2_transit_gateway and vpc-owned aws_vpc_vpn_gateway nodes by
// AWS-reported ID.
//
// Two secret classes are never persisted. The scanner never reads or stores the
// BGP authentication key (the AWS authKey field) on a virtual interface or any
// BGP peer; the scanner-owned VirtualInterface type has no field for it. The
// scanner never stores MACsec connectivity association key names (CKN) or
// secret ARNs on connections or LAGs; only the boolean MACsec capability flag
// is surfaced. Direct Connect therefore needs no redaction key.
package directconnect
