// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDirectConnect identifies the regional AWS Direct Connect metadata
	// scan slice. The slice covers connections, virtual interfaces, Direct
	// Connect gateways, and link aggregation groups (LAGs). Direct Connect is a
	// hybrid-networking family: virtual interfaces attach to Direct Connect
	// gateways, and Direct Connect gateways associate with transit gateways or
	// virtual private gateways owned by the transitgateway and vpc scanners.
	ServiceDirectConnect = "directconnect"
)

const (
	// ResourceTypeDirectConnectConnection identifies a Direct Connect physical
	// connection (port) metadata resource.
	ResourceTypeDirectConnectConnection = "aws_direct_connect_connection"
	// ResourceTypeDirectConnectVirtualInterface identifies a Direct Connect
	// virtual interface metadata resource. The virtual_interface_type attribute
	// records the AWS-reported variant (private, public, or transit). The
	// scanner never persists the BGP authentication key.
	ResourceTypeDirectConnectVirtualInterface = "aws_direct_connect_virtual_interface"
	// ResourceTypeDirectConnectGateway identifies a Direct Connect gateway
	// metadata resource. The value matches the target type the transitgateway
	// scanner already emits for transit_gateway_attachment_to_direct_connect_gateway
	// edges, so the node that edge points at becomes real once this scanner runs.
	ResourceTypeDirectConnectGateway = "aws_direct_connect_gateway"
	// ResourceTypeDirectConnectLAG identifies a Direct Connect link aggregation
	// group (LAG) metadata resource.
	ResourceTypeDirectConnectLAG = "aws_direct_connect_lag"
)

const (
	// RelationshipDirectConnectVirtualInterfaceToGateway records a virtual
	// interface's reported Direct Connect gateway. The target is the
	// aws_direct_connect_gateway identity owned by this scanner.
	RelationshipDirectConnectVirtualInterfaceToGateway = "direct_connect_virtual_interface_to_direct_connect_gateway"
	// RelationshipDirectConnectVirtualInterfaceToConnection records a virtual
	// interface's reported parent connection (the physical port the virtual
	// interface runs over). The target is the aws_direct_connect_connection
	// identity owned by this scanner.
	RelationshipDirectConnectVirtualInterfaceToConnection = "direct_connect_virtual_interface_to_connection"
	// RelationshipDirectConnectConnectionInLAG records a connection's reported
	// parent LAG. The target is the aws_direct_connect_lag identity owned by
	// this scanner.
	RelationshipDirectConnectConnectionInLAG = "direct_connect_connection_in_lag"
	// RelationshipDirectConnectGatewayToTransitGateway records a Direct Connect
	// gateway association whose associated gateway is a transit gateway. The
	// target is the transitgateway-scanner-owned aws_ec2_transit_gateway
	// identity, so the edge joins the transit gateway node by AWS-reported ID.
	RelationshipDirectConnectGatewayToTransitGateway = "direct_connect_gateway_to_transit_gateway"
	// RelationshipDirectConnectGatewayToVPNGateway records a Direct Connect
	// gateway association whose associated gateway is a virtual private gateway.
	// The target is the vpc-scanner-owned aws_vpc_vpn_gateway identity, so the
	// edge joins the virtual private gateway node by AWS-reported ID.
	RelationshipDirectConnectGatewayToVPNGateway = "direct_connect_gateway_to_vpn_gateway"
)
