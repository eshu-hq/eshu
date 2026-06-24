// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceTransitGateway identifies the regional AWS Transit Gateway
	// metadata scan slice. The slice covers transit gateways, transit gateway
	// route tables, transit gateway attachments (VPC, VPN, Direct Connect,
	// peering, and Connect), transit gateway peering attachments, multicast
	// domains, and policy tables. VPC route tables and VPN connections stay
	// with the VPC scanner; the two scanners cross-reference by AWS-reported
	// identifier.
	ServiceTransitGateway = "transitgateway"
)

const (
	// ResourceTypeTransitGateway identifies a transit gateway metadata
	// resource. The value matches the target type the VPC scanner already
	// emits for vpc_route_targets_transit_gateway and
	// vpc_vpn_connection_uses_transit_gateway edges, so the node those edges
	// point at becomes real once this scanner runs.
	ResourceTypeTransitGateway = "aws_ec2_transit_gateway"
	// ResourceTypeTransitGatewayRouteTable identifies a transit gateway route
	// table metadata resource. This is distinct from the VPC-owned
	// aws_vpc_route_table; a transit gateway route table routes between transit
	// gateway attachments, not between subnets.
	ResourceTypeTransitGatewayRouteTable = "aws_ec2_transit_gateway_route_table"
	// ResourceTypeTransitGatewayAttachment identifies a transit gateway
	// attachment metadata resource. The resource_type covers VPC, VPN, Direct
	// Connect gateway, and Connect attachments; the attachment_resource_type
	// attribute records the AWS-reported variant. Peering attachments use the
	// dedicated peering resource type below.
	ResourceTypeTransitGatewayAttachment = "aws_ec2_transit_gateway_attachment"
	// ResourceTypeTransitGatewayPeeringAttachment identifies a transit gateway
	// peering attachment metadata resource. Peering attachments carry
	// requester and accepter transit gateway info that can cross accounts and
	// Regions, so they are modeled separately from same-account attachments.
	ResourceTypeTransitGatewayPeeringAttachment = "aws_ec2_transit_gateway_peering_attachment"
	// ResourceTypeTransitGatewayMulticastDomain identifies a transit gateway
	// multicast domain metadata resource.
	ResourceTypeTransitGatewayMulticastDomain = "aws_ec2_transit_gateway_multicast_domain"
	// ResourceTypeTransitGatewayPolicyTable identifies a transit gateway policy
	// table metadata resource. The scanner emits identity and state only; it
	// never reads the policy entries.
	ResourceTypeTransitGatewayPolicyTable = "aws_ec2_transit_gateway_policy_table"
)

const (
	// RelationshipTransitGatewayRouteTableInTransitGateway records a transit
	// gateway route table's reported parent transit gateway.
	RelationshipTransitGatewayRouteTableInTransitGateway = "transit_gateway_route_table_in_transit_gateway"
	// RelationshipTransitGatewayAttachmentToTransitGateway records an
	// attachment's reported parent transit gateway.
	RelationshipTransitGatewayAttachmentToTransitGateway = "transit_gateway_attachment_to_transit_gateway"
	// RelationshipTransitGatewayAttachmentToVPC records a VPC attachment's
	// reported VPC. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipTransitGatewayAttachmentToVPC = "transit_gateway_attachment_to_vpc"
	// RelationshipTransitGatewayAttachmentToVPNConnection records a VPN
	// attachment's reported VPN connection. The target is the VPC-scanner-owned
	// aws_vpc_vpn_connection identity.
	RelationshipTransitGatewayAttachmentToVPNConnection = "transit_gateway_attachment_to_vpn_connection"
	// RelationshipTransitGatewayAttachmentToDirectConnectGateway records a
	// Direct Connect gateway attachment's reported Direct Connect gateway.
	RelationshipTransitGatewayAttachmentToDirectConnectGateway = "transit_gateway_attachment_to_direct_connect_gateway"
	// RelationshipTransitGatewayAttachmentToPeer records a generic attachment
	// whose AWS-reported resource type is a peering attachment, linking the
	// attachment to the peer transit gateway attachment identity.
	RelationshipTransitGatewayAttachmentToPeer = "transit_gateway_attachment_to_peer"
	// RelationshipTransitGatewayMulticastDomainInTransitGateway records a
	// multicast domain's reported parent transit gateway.
	RelationshipTransitGatewayMulticastDomainInTransitGateway = "transit_gateway_multicast_domain_in_transit_gateway"
	// RelationshipTransitGatewayPolicyTableInTransitGateway records a policy
	// table's reported parent transit gateway.
	RelationshipTransitGatewayPolicyTableInTransitGateway = "transit_gateway_policy_table_in_transit_gateway"
	// RelationshipTransitGatewayRouteTableToAttachment records a transit
	// gateway route table's association to an attachment reported by AWS.
	RelationshipTransitGatewayRouteTableToAttachment = "transit_gateway_route_table_to_attachment"
	// RelationshipTransitGatewayPeeringRequestsTransitGateway records the
	// requester side of a transit gateway peering attachment. The target is the
	// requester transit gateway; the relationship attributes carry the reported
	// owner account and Region for cross-account org-context joins.
	RelationshipTransitGatewayPeeringRequestsTransitGateway = "transit_gateway_peering_requests_transit_gateway"
	// RelationshipTransitGatewayPeeringAcceptsTransitGateway records the
	// accepter side of a transit gateway peering attachment. The accepter is
	// frequently a transit gateway in a different account; the relationship is
	// emitted with the cross-account identity as reported by AWS and flagged for
	// downstream org-context resolution. The scanner never resolves the remote
	// account identity itself.
	RelationshipTransitGatewayPeeringAcceptsTransitGateway = "transit_gateway_peering_accepts_transit_gateway"
)
