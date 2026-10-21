// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Network-category constants consolidated from constants_vpc.go (#6696).

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
	RelationshipVPCElasticIPAssociatedWithInstance = "vpc_elastic_ip_associated_with_instance" // #nosec G101 -- relationship-type identifier, not a credential
	// RelationshipVPCElasticIPAssociatedWithNetworkInterface records an Elastic
	// IP allocation's reported network interface association.
	RelationshipVPCElasticIPAssociatedWithNetworkInterface = "vpc_elastic_ip_associated_with_network_interface" // #nosec G101 -- relationship-type identifier, not a credential
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

// Network-category constants consolidated from constants_vpclattice.go (#6696).

const (
	// ServiceVPCLattice identifies the regional Amazon VPC Lattice metadata-only
	// scan slice. The scanner reads service network, service, target group, and
	// listener control-plane metadata through the VPC Lattice list/get management
	// APIs and never reads or persists auth-policy bodies, resource-policy bodies,
	// or any data-plane payload, and never mutates VPC Lattice state.
	ServiceVPCLattice = "vpclattice"
)

const (
	// ResourceTypeVPCLatticeServiceNetwork identifies an Amazon VPC Lattice
	// service network metadata resource. The scanner emits identity, associated
	// service/VPC/resource-configuration counts, and lifecycle timestamps only.
	ResourceTypeVPCLatticeServiceNetwork = "aws_vpclattice_service_network"
	// ResourceTypeVPCLatticeService identifies an Amazon VPC Lattice service
	// metadata resource. The scanner emits identity, status, custom domain name,
	// DNS entry domain name, auth type, and the ACM certificate ARN reference
	// only; it never reads the auth-policy body.
	ResourceTypeVPCLatticeService = "aws_vpclattice_service"
	// ResourceTypeVPCLatticeTargetGroup identifies an Amazon VPC Lattice target
	// group metadata resource. The scanner emits identity, type (IP, LAMBDA,
	// INSTANCE, ALB), protocol, port, IP address type, status, and the backing
	// VPC identifier only.
	ResourceTypeVPCLatticeTargetGroup = "aws_vpclattice_target_group"
	// ResourceTypeVPCLatticeListener identifies an Amazon VPC Lattice listener
	// metadata resource. The scanner emits identity, protocol, port, and the
	// parent service identity only; it never expands listener rule action bodies.
	ResourceTypeVPCLatticeListener = "aws_vpclattice_listener"
)

const (
	// RelationshipVPCLatticeServiceNetworkAssociatesVPC records a VPC Lattice
	// service network's association with a VPC. The target is the EC2-owned
	// aws_ec2_vpc identity keyed by the bare vpc-id the EC2 scanner publishes.
	RelationshipVPCLatticeServiceNetworkAssociatesVPC = "vpclattice_service_network_associates_vpc"
	// RelationshipVPCLatticeServiceNetworkAssociatesService records a VPC Lattice
	// service network's association with a service. The target is the VPC Lattice
	// service identity keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeServiceNetworkAssociatesService = "vpclattice_service_network_associates_service"
	// RelationshipVPCLatticeListenerInService records a VPC Lattice listener's
	// membership in its parent service. The target is the VPC Lattice service
	// identity keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeListenerInService = "vpclattice_listener_in_service"
	// RelationshipVPCLatticeTargetGroupInVPC records a VPC Lattice target group's
	// backing VPC. The target is the EC2-owned aws_ec2_vpc identity keyed by the
	// bare vpc-id the EC2 scanner publishes.
	RelationshipVPCLatticeTargetGroupInVPC = "vpclattice_target_group_in_vpc"
	// RelationshipVPCLatticeTargetGroupServesService records a VPC Lattice target
	// group's use by a service. The target is the VPC Lattice service identity
	// keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeTargetGroupServesService = "vpclattice_target_group_serves_service"
	// RelationshipVPCLatticeTargetGroupTargetsLambda records a VPC Lattice target
	// group's registered Lambda function target. The target is the
	// aws_lambda_function identity keyed by the function ARN the Lambda scanner
	// publishes.
	RelationshipVPCLatticeTargetGroupTargetsLambda = "vpclattice_target_group_targets_lambda"
	// RelationshipVPCLatticeTargetGroupTargetsInstance records a VPC Lattice
	// target group's registered EC2 instance target. The target is the
	// aws_ec2_instance identity keyed by the bare instance id (i-...).
	RelationshipVPCLatticeTargetGroupTargetsInstance = "vpclattice_target_group_targets_instance"
	// RelationshipVPCLatticeTargetGroupTargetsLoadBalancer records a VPC Lattice
	// target group's registered Application Load Balancer target. The target is
	// the aws_elbv2_load_balancer identity keyed by the load balancer ARN the
	// ELBv2 scanner publishes.
	RelationshipVPCLatticeTargetGroupTargetsLoadBalancer = "vpclattice_target_group_targets_load_balancer"
	// RelationshipVPCLatticeServiceUsesCertificate records a VPC Lattice service's
	// custom-domain ACM certificate. The target is the aws_acm_certificate
	// identity keyed by the certificate ARN the ACM scanner publishes.
	RelationshipVPCLatticeServiceUsesCertificate = "vpclattice_service_uses_certificate"
)

// Network-category constants consolidated from constants_route53.go (#6696).

const (
	// ServiceRoute53 identifies the global Amazon Route 53 service scan slice.
	ServiceRoute53 = "route53"
)

const (
	// ResourceTypeRoute53HostedZone identifies a Route 53 hosted zone.
	ResourceTypeRoute53HostedZone = "aws_route53_hosted_zone"
)

// Network-category constants consolidated from constants_route53resolver.go (#6696).

const (
	// ServiceRoute53Resolver identifies the regional Amazon Route 53 Resolver
	// metadata scan slice. The slice covers resolver endpoints, resolver rules
	// and rule associations, DNS Firewall rule groups and domain lists
	// (metadata and counts only), firewall rule group associations, and query
	// log configurations (destination only). It never reads DNS Firewall domain
	// list contents or query log records, and it never mutates a resource.
	//
	// This is distinct from ServiceRoute53, which owns hosted zones and DNS
	// records. Route 53 Resolver is a regional service, so the boundary carries
	// the real claim region rather than aws-global.
	ServiceRoute53Resolver = "route53resolver"
)

const (
	// ResourceTypeRoute53ResolverEndpoint identifies a Route 53 Resolver inbound
	// or outbound endpoint. The direction attribute disambiguates the variant.
	ResourceTypeRoute53ResolverEndpoint = "aws_route53resolver_endpoint"
	// ResourceTypeRoute53ResolverRule identifies a Route 53 Resolver rule. The
	// rule_type attribute carries FORWARD, SYSTEM, or RECURSIVE. The scanner
	// never persists forwarded target query data beyond the reported domain
	// name and rule shape.
	ResourceTypeRoute53ResolverRule = "aws_route53resolver_rule"
	// ResourceTypeRoute53ResolverRuleAssociation identifies a resolver
	// rule-to-VPC association reported by AWS.
	ResourceTypeRoute53ResolverRuleAssociation = "aws_route53resolver_rule_association"
	// ResourceTypeRoute53ResolverFirewallRuleGroup identifies a DNS Firewall
	// rule group. The resource carries the AWS-reported rule count only; the
	// scanner never reads the rule bodies via ListFirewallRules.
	ResourceTypeRoute53ResolverFirewallRuleGroup = "aws_route53resolver_firewall_rule_group"
	// ResourceTypeRoute53ResolverFirewallDomainList identifies a DNS Firewall
	// domain list. The resource carries the AWS-reported domain count only; the
	// scanner never reads the domain entries via ListFirewallDomains.
	ResourceTypeRoute53ResolverFirewallDomainList = "aws_route53resolver_firewall_domain_list"
	// ResourceTypeRoute53ResolverFirewallRuleGroupAssociation identifies a DNS
	// Firewall rule group-to-VPC association reported by AWS.
	ResourceTypeRoute53ResolverFirewallRuleGroupAssociation = "aws_route53resolver_firewall_rule_group_association" // #nosec G101 -- AWS resource-type identifier constant, not a credential
	// ResourceTypeRoute53ResolverQueryLogConfig identifies a Resolver query log
	// configuration. The resource carries the reported destination ARN only;
	// the scanner never reads query log records.
	ResourceTypeRoute53ResolverQueryLogConfig = "aws_route53resolver_query_log_config" // #nosec G101 -- resource-type identifier, not a credential
)

const (
	// RelationshipRoute53ResolverEndpointInVPC records a resolver endpoint's
	// reported host VPC placement.
	RelationshipRoute53ResolverEndpointInVPC = "route53resolver_endpoint_in_vpc"
	// RelationshipRoute53ResolverEndpointUsesSubnet records a subnet reported by
	// one of a resolver endpoint's IP addresses.
	RelationshipRoute53ResolverEndpointUsesSubnet = "route53resolver_endpoint_uses_subnet"
	// RelationshipRoute53ResolverRuleUsesEndpoint records the outbound resolver
	// endpoint a FORWARD rule routes queries through.
	RelationshipRoute53ResolverRuleUsesEndpoint = "route53resolver_rule_uses_endpoint"
	// RelationshipRoute53ResolverRuleAssociationTargetsVPC records the VPC a
	// resolver rule association binds the rule to.
	RelationshipRoute53ResolverRuleAssociationTargetsVPC = "route53resolver_rule_association_targets_vpc"
	// RelationshipRoute53ResolverRuleAssociationUsesRule records the resolver
	// rule a rule association binds to a VPC.
	RelationshipRoute53ResolverRuleAssociationUsesRule = "route53resolver_rule_association_uses_rule"
	// RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC records
	// the VPC a DNS Firewall rule group association binds the rule group to.
	RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC = "route53resolver_firewall_rule_group_association_targets_vpc"
	// RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup
	// records the DNS Firewall rule group a rule group association binds to a
	// VPC.
	RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup = "route53resolver_firewall_rule_group_association_uses_rule_group"
)

// Network-category constants consolidated from constants_route53recoverycontrolconfig.go (#6696).

const (
	// ServiceRoute53RecoveryControlConfig identifies the Amazon Route 53
	// Application Recovery Controller recovery-control configuration scan slice.
	// The scanner reads the control-plane List/Describe APIs of the
	// route53recoverycontrolconfig service (ListClusters, ListControlPanels,
	// ListRoutingControls, ListSafetyRules, ListTagsForResource) and never
	// changes a routing control state, never reads routing control state through
	// the separate route53recoverycluster data-plane endpoint, and never mutates
	// any recovery-control configuration resource. This is distinct from
	// ServiceRoute53 (hosted zones / DNS) and ServiceRoute53Resolver.
	ServiceRoute53RecoveryControlConfig = "route53recoverycontrolconfig"
)

const (
	// ResourceTypeRoute53RecoveryControlConfigCluster identifies a Route 53
	// Application Recovery Controller cluster: the highly available data plane
	// that hosts routing control states. The scanner emits identity (cluster
	// ARN, name), deployment status, network type, owner account, and the set of
	// regional cluster endpoint Regions (never used to read or set routing
	// control state).
	ResourceTypeRoute53RecoveryControlConfigCluster = "aws_route53recoverycontrolconfig_cluster"
	// ResourceTypeRoute53RecoveryControlConfigControlPanel identifies a Route 53
	// Application Recovery Controller control panel: a group of routing controls
	// that change together within one cluster. The scanner emits identity
	// (control panel ARN, name), owning cluster ARN, deployment status, the
	// default-control-panel flag, routing control count, and owner account.
	ResourceTypeRoute53RecoveryControlConfigControlPanel = "aws_route53recoverycontrolconfig_control_panel"
	// ResourceTypeRoute53RecoveryControlConfigRoutingControl identifies a Route 53
	// Application Recovery Controller routing control: the on/off switch whose
	// state directs traffic for an application failover. The scanner emits
	// identity (routing control ARN, name), owning control panel ARN, deployment
	// status, and owner account only; it never reads or persists the routing
	// control's live On/Off state, which lives behind the data-plane endpoint.
	ResourceTypeRoute53RecoveryControlConfigRoutingControl = "aws_route53recoverycontrolconfig_routing_control"
	// ResourceTypeRoute53RecoveryControlConfigSafetyRule identifies a Route 53
	// Application Recovery Controller safety rule (an assertion rule or a gating
	// rule) that guards routing control state changes within a control panel. The
	// scanner emits identity (safety rule ARN, name), owning control panel ARN,
	// rule kind (ASSERTION/GATING), deployment status, the rule-config logic
	// (type ATLEAST/AND/OR, threshold, inverted flag), the wait period, and the
	// counts of asserted/gating/target routing controls. It records counts and
	// rule logic only, never application traffic or routing control state.
	ResourceTypeRoute53RecoveryControlConfigSafetyRule = "aws_route53recoverycontrolconfig_safety_rule"
)

const (
	// RelationshipRoute53RecoveryControlConfigControlPanelInCluster records a
	// control panel's membership in its owning cluster. The target is keyed by
	// the cluster ARN the cluster node publishes as its resource_id, so the edge
	// joins the cluster node exactly.
	RelationshipRoute53RecoveryControlConfigControlPanelInCluster = "route53recoverycontrolconfig_control_panel_in_cluster"
	// RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel records
	// a routing control's membership in its owning control panel. The target is
	// keyed by the control panel ARN the control panel node publishes as its
	// resource_id.
	RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel = "route53recoverycontrolconfig_routing_control_in_control_panel"
	// RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel records a
	// safety rule's membership in the control panel it guards. The target is keyed
	// by the control panel ARN the control panel node publishes as its
	// resource_id.
	RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel = "route53recoverycontrolconfig_safety_rule_in_control_panel"
)

// Network-category constants consolidated from constants_directconnect.go (#6696).

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

// Network-category constants consolidated from constants_transitgateway.go (#6696).

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
