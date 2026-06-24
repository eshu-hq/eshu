// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
