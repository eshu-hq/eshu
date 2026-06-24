// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceVPC identifies the regional Amazon VPC network-fabric metadata
	// scan slice. The slice covers route tables, internet gateways, NAT
	// gateways, network ACLs, VPC peering connections, VPC endpoints, Elastic
	// IPs, DHCP option sets, customer gateways, VPN gateways, and VPN
	// connections. VPCs, subnets, security groups, security group rules, and
	// network interfaces stay with the EC2 service slice.
	ServiceVPC = "vpc"
)

const (
	// ResourceTypeVPCRouteTable identifies a VPC route table metadata resource.
	ResourceTypeVPCRouteTable = "aws_vpc_route_table"
	// ResourceTypeVPCInternetGateway identifies an internet gateway metadata
	// resource.
	ResourceTypeVPCInternetGateway = "aws_vpc_internet_gateway"
	// ResourceTypeVPCNATGateway identifies a NAT gateway metadata resource.
	ResourceTypeVPCNATGateway = "aws_vpc_nat_gateway"
	// ResourceTypeVPCNetworkACL identifies a network ACL metadata resource.
	ResourceTypeVPCNetworkACL = "aws_vpc_network_acl"
	// ResourceTypeVPCPeeringConnection identifies a VPC peering connection
	// metadata resource.
	ResourceTypeVPCPeeringConnection = "aws_vpc_peering_connection"
	// ResourceTypeVPCEndpoint identifies a VPC endpoint metadata resource. The
	// resource_type covers both gateway and interface endpoints; the
	// endpoint_type attribute disambiguates the AWS-reported variant.
	ResourceTypeVPCEndpoint = "aws_vpc_endpoint"
	// ResourceTypeVPCElasticIP identifies a VPC elastic IP allocation metadata
	// resource.
	ResourceTypeVPCElasticIP = "aws_vpc_elastic_ip"
	// ResourceTypeVPCDHCPOptions identifies a VPC DHCP option set metadata
	// resource.
	ResourceTypeVPCDHCPOptions = "aws_vpc_dhcp_options"
	// ResourceTypeVPCCustomerGateway identifies a VPN customer gateway metadata
	// resource.
	ResourceTypeVPCCustomerGateway = "aws_vpc_customer_gateway"
	// ResourceTypeVPCVPNGateway identifies a virtual private gateway metadata
	// resource.
	ResourceTypeVPCVPNGateway = "aws_vpc_vpn_gateway"
	// ResourceTypeVPCVPNConnection identifies a site-to-site VPN connection
	// metadata resource. The scanner emits identity, configuration profile, and
	// reported endpoint metadata only; tunnel pre-shared keys are never
	// persisted.
	ResourceTypeVPCVPNConnection = "aws_vpc_vpn_connection"
)

const (
	// RelationshipVPCRouteTableInVPC records route-table membership in a VPC.
	RelationshipVPCRouteTableInVPC = "vpc_route_table_in_vpc"
	// RelationshipVPCRouteTableAssociatedWithSubnet records a route-table-to-subnet
	// association reported by AWS.
	RelationshipVPCRouteTableAssociatedWithSubnet = "vpc_route_table_associated_with_subnet"
	// RelationshipVPCRouteTargetsInternetGateway records an internet gateway
	// reported as a route target by a route table.
	RelationshipVPCRouteTargetsInternetGateway = "vpc_route_targets_internet_gateway"
	// RelationshipVPCRouteTargetsNATGateway records a NAT gateway reported as a
	// route target by a route table.
	RelationshipVPCRouteTargetsNATGateway = "vpc_route_targets_nat_gateway"
	// RelationshipVPCRouteTargetsPeeringConnection records a VPC peering
	// connection reported as a route target by a route table.
	RelationshipVPCRouteTargetsPeeringConnection = "vpc_route_targets_peering_connection"
	// RelationshipVPCRouteTargetsVPCEndpoint records a VPC endpoint reported as
	// a route target by a route table.
	RelationshipVPCRouteTargetsVPCEndpoint = "vpc_route_targets_vpc_endpoint"
	// RelationshipVPCRouteTargetsTransitGateway records a transit gateway
	// reported as a route target by a route table.
	RelationshipVPCRouteTargetsTransitGateway = "vpc_route_targets_transit_gateway"
	// RelationshipVPCInternetGatewayAttachedToVPC records an internet gateway's
	// reported VPC attachment.
	RelationshipVPCInternetGatewayAttachedToVPC = "vpc_internet_gateway_attached_to_vpc"
	// RelationshipVPCNATGatewayInSubnet records a NAT gateway's reported subnet
	// placement.
	RelationshipVPCNATGatewayInSubnet = "vpc_nat_gateway_in_subnet"
	// RelationshipVPCNATGatewayInVPC records a NAT gateway's reported VPC
	// placement.
	RelationshipVPCNATGatewayInVPC = "vpc_nat_gateway_in_vpc"
	// RelationshipVPCNetworkACLInVPC records network-ACL membership in a VPC.
	RelationshipVPCNetworkACLInVPC = "vpc_network_acl_in_vpc"
	// RelationshipVPCNetworkACLAssociatedWithSubnet records a network-ACL
	// association to a subnet reported by AWS.
	RelationshipVPCNetworkACLAssociatedWithSubnet = "vpc_network_acl_associated_with_subnet"
	// RelationshipVPCPeeringConnectsVPC records one side of a VPC peering
	// connection. The scanner emits one edge per requester/accepter VPC the API
	// reports.
	RelationshipVPCPeeringConnectsVPC = "vpc_peering_connects_vpc"
	// RelationshipVPCEndpointInVPC records a VPC endpoint's parent VPC
	// placement.
	RelationshipVPCEndpointInVPC = "vpc_endpoint_in_vpc"
	// RelationshipVPCEndpointUsesService records the AWS service name reported
	// by a VPC endpoint.
	RelationshipVPCEndpointUsesService = "vpc_endpoint_uses_service"
	// RelationshipVPCElasticIPAssociatedWithInstance records an Elastic IP
	// allocation's reported EC2 instance association.
	RelationshipVPCElasticIPAssociatedWithInstance = "vpc_elastic_ip_associated_with_instance"
	// RelationshipVPCElasticIPAssociatedWithNetworkInterface records an Elastic
	// IP allocation's reported network interface association.
	RelationshipVPCElasticIPAssociatedWithNetworkInterface = "vpc_elastic_ip_associated_with_network_interface"
	// RelationshipVPCVPNGatewayAttachedToVPC records a virtual private gateway's
	// reported VPC attachment.
	RelationshipVPCVPNGatewayAttachedToVPC = "vpc_vpn_gateway_attached_to_vpc"
	// RelationshipVPCVPNConnectionUsesCustomerGateway records a VPN connection's
	// reported customer-gateway endpoint.
	RelationshipVPCVPNConnectionUsesCustomerGateway = "vpc_vpn_connection_uses_customer_gateway"
	// RelationshipVPCVPNConnectionUsesVPNGateway records a VPN connection's
	// reported virtual-private-gateway endpoint.
	RelationshipVPCVPNConnectionUsesVPNGateway = "vpc_vpn_connection_uses_vpn_gateway"
	// RelationshipVPCVPNConnectionUsesTransitGateway records a VPN connection's
	// reported transit-gateway endpoint when AWS reports a transit-gateway ID.
	RelationshipVPCVPNConnectionUsesTransitGateway = "vpc_vpn_connection_uses_transit_gateway"
)
