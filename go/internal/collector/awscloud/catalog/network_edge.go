// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Network-edge-category constants consolidated from constants_networkmanager.go (#6696).

const (
	// ServiceNetworkManager identifies the global AWS Network Manager
	// metadata-only scan slice. AWS Network Manager is a global service whose
	// control plane is reachable only in a single region per partition
	// (us-west-2 for commercial), so the SDK adapter pins that region while the
	// scan boundary keeps its claimed account and region for attribution. The
	// scanner reads global-network, site, device, link, connection, core-network,
	// and transit-gateway-registration control-plane metadata through the
	// Describe/Get/List management APIs and never mutates Network Manager state.
	ServiceNetworkManager = "networkmanager"
)

const (
	// ResourceTypeNetworkManagerGlobalNetwork identifies an AWS Network Manager
	// global network metadata resource. It is the partition-global container that
	// owns sites, devices, links, connections, transit gateway registrations, and
	// core networks. The scanner emits identity, state, description, and
	// lifecycle timestamps only.
	ResourceTypeNetworkManagerGlobalNetwork = "aws_networkmanager_global_network"
	// ResourceTypeNetworkManagerSite identifies an AWS Network Manager site
	// metadata resource: an on-premises or cloud location within a global
	// network. The scanner emits identity, parent global-network reference,
	// physical location (address/latitude/longitude reported by the operator),
	// state, and timestamps only.
	ResourceTypeNetworkManagerSite = "aws_networkmanager_site"
	// ResourceTypeNetworkManagerDevice identifies an AWS Network Manager device
	// metadata resource: a physical or virtual appliance within a global network.
	// The scanner emits identity, parent global-network and site references,
	// vendor/model/type, the reported AWS subnet/zone location, state, and
	// timestamps only. Serial numbers are operator-supplied inventory metadata
	// and are intentionally omitted.
	ResourceTypeNetworkManagerDevice = "aws_networkmanager_device"
	// ResourceTypeNetworkManagerLink identifies an AWS Network Manager link
	// metadata resource: a connection medium (for example an MPLS or internet
	// circuit) at a site. The scanner emits identity, parent global-network and
	// site references, provider, type, bandwidth, state, and timestamps only.
	ResourceTypeNetworkManagerLink = "aws_networkmanager_link"
	// ResourceTypeNetworkManagerConnection identifies an AWS Network Manager
	// connection metadata resource: a physical cabling association between two
	// devices in a global network. The scanner emits identity, parent
	// global-network reference, the connected device and link references, state,
	// and timestamps only.
	ResourceTypeNetworkManagerConnection = "aws_networkmanager_connection"
	// ResourceTypeNetworkManagerCoreNetwork identifies an AWS Network Manager
	// core network metadata resource: the AWS-managed Cloud WAN backbone of a
	// global network. The scanner emits identity, parent global-network
	// reference, state, description, and segment/edge name counts only; routing
	// policy documents are intentionally excluded.
	ResourceTypeNetworkManagerCoreNetwork = "aws_networkmanager_core_network"
)

const (
	// RelationshipNetworkManagerCoreNetworkInGlobalNetwork records a core
	// network's membership in its parent global network. The target is keyed by
	// the parent global-network ARN so the edge joins the global-network node the
	// scanner publishes.
	RelationshipNetworkManagerCoreNetworkInGlobalNetwork = "networkmanager_core_network_in_global_network"
	// RelationshipNetworkManagerSiteInGlobalNetwork records a site's membership in
	// its parent global network, keyed by the parent global-network ARN.
	RelationshipNetworkManagerSiteInGlobalNetwork = "networkmanager_site_in_global_network"
	// RelationshipNetworkManagerDeviceInGlobalNetwork records a device's
	// membership in its parent global network, keyed by the parent global-network
	// ARN.
	RelationshipNetworkManagerDeviceInGlobalNetwork = "networkmanager_device_in_global_network"
	// RelationshipNetworkManagerLinkInGlobalNetwork records a link's membership in
	// its parent global network, keyed by the parent global-network ARN.
	RelationshipNetworkManagerLinkInGlobalNetwork = "networkmanager_link_in_global_network"
	// RelationshipNetworkManagerConnectionInGlobalNetwork records a connection's
	// membership in its parent global network, keyed by the parent global-network
	// ARN.
	RelationshipNetworkManagerConnectionInGlobalNetwork = "networkmanager_connection_in_global_network"
	// RelationshipNetworkManagerDeviceInSite records a device's placement at a
	// site, keyed by the site ARN the site node publishes.
	RelationshipNetworkManagerDeviceInSite = "networkmanager_device_in_site"
	// RelationshipNetworkManagerLinkInSite records a link's placement at a site,
	// keyed by the site ARN the site node publishes.
	RelationshipNetworkManagerLinkInSite = "networkmanager_link_in_site"
	// RelationshipNetworkManagerDeviceUsesLink records a device-to-link
	// association reported by GetLinkAssociations, keyed by the link ARN the link
	// node publishes.
	RelationshipNetworkManagerDeviceUsesLink = "networkmanager_device_uses_link"
	// RelationshipNetworkManagerConnectionConnectsDevice records a connection's
	// reference to one of its two endpoint devices, keyed by the device ARN the
	// device node publishes.
	RelationshipNetworkManagerConnectionConnectsDevice = "networkmanager_connection_connects_device"
	// RelationshipNetworkManagerGlobalNetworkRegistersTransitGateway records a
	// transit gateway's registration into a global network. The target is the
	// transit-gateway-scanner-owned identity, keyed by the bare transit gateway id
	// (tgw-...) extracted from the registration's transit gateway ARN, because the
	// transit gateway node publishes its resource_id as the bare id, not an ARN.
	RelationshipNetworkManagerGlobalNetworkRegistersTransitGateway = "networkmanager_global_network_registers_transit_gateway"
)

// Network-edge-category constants consolidated from constants_networkfirewall.go (#6696).

const (
	// ServiceNetworkFirewall identifies the regional AWS Network Firewall
	// metadata scan slice. One claim scans firewalls, firewall policies, rule
	// groups, and TLS inspection configurations for its boundary account and
	// region. Rule group rule sources (Suricata signature bodies) are never
	// read or persisted because they are threat-detection intelligence.
	ServiceNetworkFirewall = "networkfirewall"
)

const (
	// ResourceTypeNetworkFirewallFirewall identifies a Network Firewall firewall
	// metadata resource.
	ResourceTypeNetworkFirewallFirewall = "aws_networkfirewall_firewall"
	// ResourceTypeNetworkFirewallPolicy identifies a Network Firewall firewall
	// policy metadata resource. The resource carries identity and default-action
	// names only; the full policy rule body is never persisted.
	ResourceTypeNetworkFirewallPolicy = "aws_networkfirewall_firewall_policy"
	// ResourceTypeNetworkFirewallRuleGroup identifies a Network Firewall rule
	// group metadata resource. The resource carries identity, type
	// (STATEFUL/STATELESS), and capacity only; the rule source (Suricata
	// signature bodies) is never read or persisted because it is threat
	// intelligence.
	ResourceTypeNetworkFirewallRuleGroup = "aws_networkfirewall_rule_group"
	// ResourceTypeNetworkFirewallTLSInspectionConfiguration identifies a Network
	// Firewall TLS inspection configuration metadata resource. The resource
	// carries identity and aggregate metadata only; certificate bodies and TLS
	// inspection scope rule bodies are never persisted.
	ResourceTypeNetworkFirewallTLSInspectionConfiguration = "aws_networkfirewall_tls_inspection_configuration"
)

const (
	// RelationshipNetworkFirewallFirewallInVPC records a firewall's reported VPC
	// placement. The target is the bare VPC id and resolves to an EC2-owned VPC.
	RelationshipNetworkFirewallFirewallInVPC = "networkfirewall_firewall_in_vpc"
	// RelationshipNetworkFirewallFirewallUsesSubnet records a firewall's reported
	// subnet mapping. The target is the bare subnet id and resolves to an
	// EC2-owned subnet.
	RelationshipNetworkFirewallFirewallUsesSubnet = "networkfirewall_firewall_uses_subnet"
	// RelationshipNetworkFirewallFirewallUsesPolicy records a firewall's reference
	// to its firewall policy by ARN.
	RelationshipNetworkFirewallFirewallUsesPolicy = "networkfirewall_firewall_uses_policy"
	// RelationshipNetworkFirewallPolicyUsesRuleGroup records a firewall policy's
	// reference to a stateful or stateless rule group by ARN.
	RelationshipNetworkFirewallPolicyUsesRuleGroup = "networkfirewall_policy_uses_rule_group"
	// RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration records a
	// firewall policy's reference to its TLS inspection configuration by ARN.
	RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration = "networkfirewall_policy_uses_tls_inspection_configuration"
)

// Network-edge-category constants consolidated from constants_elb.go (#6696).

const (
	// ServiceELB identifies the regional Classic Load Balancing (ELB v1) metadata
	// scan slice. It is distinct from ServiceELBv2, which scans Application and
	// Network Load Balancers. A claim is scanned by exactly one of the two slices;
	// they never share a service_kind.
	ServiceELB = "elb"
)

const (
	// ResourceTypeELBLoadBalancer identifies a Classic (v1) Elastic Load
	// Balancer. Classic ELBs have no AWS-assigned ARN, so the scanner synthesizes
	// a partition-aware
	// arn:<partition>:elasticloadbalancing:<region>:<account>:loadbalancer/<name>
	// ARN for the resource node and for ARN-equality joins.
	ResourceTypeELBLoadBalancer = "aws_elb_load_balancer"
)

const (
	// RelationshipELBLoadBalancerRegistersInstance records a Classic ELB's
	// reported registered EC2 instance. The edge targets aws_ec2_instance by the
	// bare instance id (i-...) the EC2 instance surface publishes.
	RelationshipELBLoadBalancerRegistersInstance = "elb_load_balancer_registers_instance"
	// RelationshipELBLoadBalancerInSubnet records a Classic ELB's reported subnet
	// placement. The edge targets aws_ec2_subnet by the bare subnet id
	// (subnet-...) the EC2 scanner publishes.
	RelationshipELBLoadBalancerInSubnet = "elb_load_balancer_in_subnet"
	// RelationshipELBLoadBalancerUsesSecurityGroup records a Classic ELB's
	// reported security group attachment. The edge targets aws_ec2_security_group
	// by the bare security group id (sg-...) the EC2 scanner publishes.
	RelationshipELBLoadBalancerUsesSecurityGroup = "elb_load_balancer_uses_security_group"
	// RelationshipELBLoadBalancerInVPC records a Classic ELB's reported VPC
	// placement. The edge targets aws_ec2_vpc by the bare VPC id (vpc-...) the EC2
	// scanner publishes.
	RelationshipELBLoadBalancerInVPC = "elb_load_balancer_in_vpc"
	// RelationshipELBLoadBalancerUsesACMCertificate records an HTTPS/SSL listener's
	// reported ACM server certificate. The edge targets aws_acm_certificate by the
	// certificate ARN the ACM scanner publishes.
	RelationshipELBLoadBalancerUsesACMCertificate = "elb_load_balancer_uses_acm_certificate"
	// RelationshipELBLoadBalancerUsesIAMServerCertificate records an HTTPS/SSL
	// listener's reported IAM server certificate. The edge targets
	// aws_iam_server_certificate by the certificate ARN. IAM server certificates
	// are a forward reference (no scanner emits them yet); the value is documented
	// in relguard.KnownTargetTypeAllowlist.
	RelationshipELBLoadBalancerUsesIAMServerCertificate = "elb_load_balancer_uses_iam_server_certificate"
)

// Network-edge-category constants consolidated from constants_elbv2.go (#6696).

const (
	// ServiceELBv2 identifies the regional Elastic Load Balancing v2 service
	// scan slice.
	ServiceELBv2 = "elbv2"
)

const (
	// ResourceTypeELBv2LoadBalancer identifies an ELBv2 load balancer.
	ResourceTypeELBv2LoadBalancer = "aws_elbv2_load_balancer"
	// ResourceTypeELBv2Listener identifies an ELBv2 listener.
	ResourceTypeELBv2Listener = "aws_elbv2_listener"
	// ResourceTypeELBv2TargetGroup identifies an ELBv2 target group.
	ResourceTypeELBv2TargetGroup = "aws_elbv2_target_group"
	// ResourceTypeELBv2Rule identifies an ELBv2 listener rule.
	ResourceTypeELBv2Rule = "aws_elbv2_rule"
)

const (
	// RelationshipELBv2LoadBalancerHasListener records listener membership on a
	// load balancer.
	RelationshipELBv2LoadBalancerHasListener = "elbv2_load_balancer_has_listener"
	// RelationshipELBv2ListenerHasRule records rule membership on a listener.
	RelationshipELBv2ListenerHasRule = "elbv2_listener_has_rule"
	// RelationshipELBv2ListenerRoutesToTargetGroup records listener or rule
	// routing to a target group.
	RelationshipELBv2ListenerRoutesToTargetGroup = "elbv2_listener_routes_to_target_group"
	// RelationshipELBv2TargetGroupAttachedToLoadBalancer records target group
	// attachment to a load balancer.
	RelationshipELBv2TargetGroupAttachedToLoadBalancer = "elbv2_target_group_attached_to_load_balancer"
)

// Network-edge-category constants consolidated from constants_globalaccelerator.go (#6696).

const (
	// ServiceGlobalAccelerator identifies the AWS Global Accelerator metadata
	// scan slice. Global Accelerator is a global-endpoint service whose
	// control-plane API lives only in us-west-2, so the scanner is scoped to a
	// us-west-2 claim even though the accelerators it reports are global.
	ServiceGlobalAccelerator = "globalaccelerator"
)

const (
	// ResourceTypeGlobalAcceleratorAccelerator identifies a Global Accelerator
	// accelerator metadata resource.
	ResourceTypeGlobalAcceleratorAccelerator = "aws_globalaccelerator_accelerator"
	// ResourceTypeGlobalAcceleratorListener identifies a Global Accelerator
	// listener metadata resource.
	ResourceTypeGlobalAcceleratorListener = "aws_globalaccelerator_listener"
	// ResourceTypeGlobalAcceleratorEndpointGroup identifies a Global
	// Accelerator endpoint group metadata resource.
	ResourceTypeGlobalAcceleratorEndpointGroup = "aws_globalaccelerator_endpoint_group"
	// ResourceTypeGlobalAcceleratorEndpoint identifies a Global Accelerator
	// endpoint metadata resource. Endpoints reference an ALB/NLB, an Elastic IP
	// allocation, or an EC2 instance by id; the endpoint resource records that
	// reference without claiming ownership of the referenced resource.
	ResourceTypeGlobalAcceleratorEndpoint = "aws_globalaccelerator_endpoint"
)

const (
	// RelationshipGlobalAcceleratorAcceleratorHasListener records listener
	// membership on an accelerator.
	RelationshipGlobalAcceleratorAcceleratorHasListener = "globalaccelerator_accelerator_has_listener"
	// RelationshipGlobalAcceleratorListenerHasEndpointGroup records endpoint
	// group membership on a listener.
	RelationshipGlobalAcceleratorListenerHasEndpointGroup = "globalaccelerator_listener_has_endpoint_group"
	// RelationshipGlobalAcceleratorEndpointGroupHasEndpoint records endpoint
	// membership in an endpoint group.
	RelationshipGlobalAcceleratorEndpointGroupHasEndpoint = "globalaccelerator_endpoint_group_has_endpoint"
	// RelationshipGlobalAcceleratorEndpointTargetsResource records the
	// reported endpoint target (ALB/NLB load balancer, Elastic IP allocation,
	// or EC2 instance) an endpoint routes traffic to. The target_type names the
	// referenced resource family so downstream correlation can join the edge.
	RelationshipGlobalAcceleratorEndpointTargetsResource = "globalaccelerator_endpoint_targets_resource"
)

// Network-edge-category constants consolidated from constants_servicediscovery.go (#6696).

const (
	// ServiceServiceDiscovery identifies the AWS Cloud Map (Service Discovery)
	// regional metadata-only scan slice. The scanner covers namespaces and
	// services plus the relationships Cloud Map reports directly. It never calls
	// a Cloud Map mutation API and never reads instance attribute maps, which can
	// carry caller-defined secrets.
	ServiceServiceDiscovery = "servicediscovery"
)

const (
	// ResourceTypeCloudMapNamespace identifies an AWS Cloud Map namespace
	// metadata resource. The fact carries namespace identity (id, ARN, name),
	// the namespace type (DNS_PUBLIC, DNS_PRIVATE, or HTTP), the reported service
	// count, the backing Route 53 hosted-zone id for DNS namespaces, and the HTTP
	// name for HTTP namespaces. Services join to this resource by the namespace
	// id.
	ResourceTypeCloudMapNamespace = "aws_cloud_map_namespace"

	// ResourceTypeCloudMapService identifies an AWS Cloud Map service metadata
	// resource. The fact is keyed by the "namespaceName/serviceName" identity so
	// it resolves the forward-looking App Mesh virtual-node-to-Cloud-Map-service
	// edge, which targets the same "namespace/service" join key. The fact carries
	// the service id and ARN, the parent namespace id and name, the DNS config
	// summary (routing policy and DNS record types/TTLs), and the reported
	// instance count. Instance attribute maps are never read or persisted because
	// they can hold caller-defined secrets.
	ResourceTypeCloudMapService = "aws_cloud_map_service"
)

const (
	// RelationshipCloudMapServiceInNamespace records that a Cloud Map service
	// belongs to a namespace. The target is the parent namespace resource keyed
	// by the Cloud Map namespace id.
	RelationshipCloudMapServiceInNamespace = "cloud_map_service_in_namespace"

	// RelationshipCloudMapNamespaceInHostedZone records that a Cloud Map DNS
	// namespace is backed by a Route 53 hosted zone. The target is the Route 53
	// hosted-zone resource keyed by the "/hostedzone/<id>" resource id the
	// route53 scanner emits, so the edge joins that scanner's hosted-zone
	// resource. HTTP namespaces report no hosted zone and emit no edge. Cloud Map
	// does not report the VPC association for a private DNS namespace; the VPC is
	// reached transitively through the private Route 53 hosted zone, which the
	// route53 scanner owns.
	RelationshipCloudMapNamespaceInHostedZone = "cloud_map_namespace_in_hosted_zone"
)

// Network-edge-category constants consolidated from constants_cloudfront.go (#6696).

const (
	// ServiceCloudFront identifies the global Amazon CloudFront metadata scan
	// slice.
	ServiceCloudFront = "cloudfront"
)

const (
	// ResourceTypeCloudFrontDistribution identifies a CloudFront distribution
	// metadata resource.
	ResourceTypeCloudFrontDistribution = "aws_cloudfront_distribution"
)

const (
	// RelationshipCloudFrontDistributionUsesACMCertificate records a
	// CloudFront distribution's reported ACM certificate dependency.
	RelationshipCloudFrontDistributionUsesACMCertificate = "cloudfront_distribution_uses_acm_certificate"
	// RelationshipCloudFrontDistributionUsesWAFWebACL records a CloudFront
	// distribution's reported WAF web ACL dependency.
	RelationshipCloudFrontDistributionUsesWAFWebACL = "cloudfront_distribution_uses_waf_web_acl"
)
