// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Compute networking constants (lane H): address, VPC network, subnetwork, firewall, route, router, interconnect attachment, and VPN families (#6696).
// Compute Static Address constants consolidated from extractor_address.go (#6696).
// Asset type constants for the compute Static Address typed-depth extractor and
// the relationship endpoints it derives. AssetTypeComputeInstance is declared
// in this leaf (compute family) and reused here; assetTypeComputeNetwork and
// assetTypeComputeSubnetwork are shared cross-group constants (see
// extractor/names.go, aliased in gcpcloud/compat_extractor.go) and reused here.
const (
	AssetTypeComputeAddress              = "compute.googleapis.com/Address"
	AssetTypeComputeGlobalAddress        = "compute.googleapis.com/GlobalAddress"
	AssetTypeComputeForwardingRule       = "compute.googleapis.com/ForwardingRule"
	AssetTypeComputeGlobalForwardingRule = "compute.googleapis.com/GlobalForwardingRule"
)

// Bounded provider relationship types for the Static Address edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when both
// endpoints resolve exactly.
const (
	RelationshipTypeAddressInNetwork            = "address_in_network"
	RelationshipTypeAddressInSubnetwork         = "address_in_subnetwork"
	RelationshipTypeAddressUsedByForwardingRule = "address_used_by_forwarding_rule"
	RelationshipTypeAddressUsedByInstance       = "address_used_by_instance"
)

// Compute VPC Network constants consolidated from extractor_compute_network.go (#6696).
// Bounded relationship types for VPC Network edges. They are stable provider
// relationship strings carried on gcp_cloud_relationship facts; the reducer
// materializes an edge only when both endpoints resolve exactly. Only edges
// derivable from the Network resource.data itself are emitted here: contained
// subnetworks and VPC peerings. Firewall, route, and instance attachments
// reference the network from their own resource.data and are emitted by those
// asset types' own extractors, not this one. The asset type constants
// (assetTypeComputeNetwork, assetTypeComputeSubnetwork) and
// computeResourceNamePrefix live in the extractor leaf package (aliased here
// via compat_extractor.go) so every extractor group can share them.
const (
	RelationshipTypeNetworkContainsSubnetwork = "vpc_network_contains_subnetwork"
	RelationshipTypeNetworkPeersWithNetwork   = "vpc_network_peers_with_network"
	// PeeringStateActive is the Compute Network peering state that exchanges
	// routes/connectivity. Only ACTIVE peerings become materializing edges.
	PeeringStateActive = "ACTIVE"
)

// Compute Subnetwork constants consolidated from extractor_subnetwork.go (#6696).
const (
	// Asset type constants for the compute Subnetwork typed-depth extractor and the
	// relationship endpoint it derives. The target asset type names the CAI asset
	// type of the parent-network edge so the reducer can resolve both endpoints
	// exactly before materializing.
	// RelationshipTypeSubnetworkInNetwork is the bounded provider relationship type
	// for the subnet -> parent VPC network edge carried on a gcp_cloud_relationship
	// fact. The reducer materializes the edge only when both endpoints resolve
	// exactly.
	RelationshipTypeSubnetworkInNetwork = "subnetwork_in_network"
)

// Compute Firewall constants consolidated from extractor_firewall.go (#6696).
const (
	// AssetTypeComputeFirewall is the CAI asset type for a VPC firewall rule. The
	// target network asset type (assetTypeComputeNetwork) and the compute
	// name-derivation helpers are shared with the sibling compute extractors.
	AssetTypeComputeFirewall = "compute.googleapis.com/Firewall"
	// RelationshipTypeFirewallAppliesToNetwork is the bounded provider relationship
	// type for the firewall -> enclosing VPC network edge carried on a
	// gcp_cloud_relationship fact. The reducer materializes it only when both
	// endpoints resolve exactly. The firewall's target service accounts are surfaced
	// as fingerprinted correlation anchors rather than edges: a service-account
	// email is not an exactly resolvable CAI endpoint, and the "applies to instances
	// running as SA" join is owned by the secrets/IAM layer keying on the digest.
	RelationshipTypeFirewallAppliesToNetwork = "firewall_applies_to_network"
)

// Compute Route constants consolidated from extractor_route.go (#6696).
// Asset type constants for the compute Route typed-depth extractor and the
// relationship endpoints it derives. assetTypeComputeNetwork,
// AssetTypeComputeInstance, and AssetTypeComputeForwardingRule (the next-hop
// internal-load-balancer edge target) are declared in this leaf (compute family) and reused here.
const (
	AssetTypeComputeRoute     = "compute.googleapis.com/Route"
	AssetTypeComputeVpnTunnel = "compute.googleapis.com/VpnTunnel"
)

// Bounded provider relationship types for the Route edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when both
// endpoints resolve exactly. The next-hop gateway (an internet gateway, not a CAI
// asset) and next-hop IP (a data-plane address) are kept as bounded attributes,
// not edges.
const (
	RelationshipTypeRouteInNetwork        = "route_in_network"
	RelationshipTypeRouteNextHopInstance  = "route_next_hop_instance"
	RelationshipTypeRouteNextHopVpnTunnel = "route_next_hop_vpn_tunnel"
	RelationshipTypeRouteNextHopIlb       = "route_next_hop_ilb"
)

// Compute Router constants consolidated from extractor_router.go (#6696).
// Asset type constants for the compute Router typed-depth extractor and the
// relationship endpoints it derives. assetTypeComputeNetwork and assetTypeComputeSubnetwork are shared cross-group constants (see extractor/names.go, aliased in gcpcloud/compat_extractor.go), and AssetTypeComputeVpnTunnel lives in this leaf (consolidated from extractor_route.go); all are reused here.
// AssetTypeComputeInterconnectAttachment is declared in this leaf since no sibling
// extractor for that asset type exists yet.
const (
	AssetTypeComputeRouter                 = "compute.googleapis.com/Router"
	AssetTypeComputeInterconnectAttachment = "compute.googleapis.com/InterconnectAttachment"
)

// Bounded provider relationship types for Router edges carried on
// gcp_cloud_relationship facts. The reducer materializes an edge only when
// both endpoints resolve exactly. BGP peers themselves never become edge
// endpoints: a peer names an interface (`interfaceName`), and only the
// interface's own linked resource (a VPN tunnel, an Interconnect attachment,
// or a subnetwork) is a resolvable CAI asset.
const (
	RelationshipTypeRouterInNetwork                             = "router_in_network"
	RelationshipTypeRouterInterfaceLinkedVpnTunnel              = "router_interface_linked_vpn_tunnel"
	RelationshipTypeRouterInterfaceLinkedInterconnectAttachment = "router_interface_linked_interconnect_attachment"
	RelationshipTypeRouterInterfaceSubnetwork                   = "router_interface_subnetwork"
)

// Compute Interconnect Attachment constants consolidated from extractor_interconnect_attachment.go (#6696).
const (
	// AssetTypeComputeInterconnect is the CAI asset type for the underlying
	// Dedicated/Cross-Cloud Interconnect a Compute InterconnectAttachment
	// traverses. AssetTypeComputeInterconnectAttachment and AssetTypeComputeRouter
	// live in this leaf (consolidated from extractor_router.go, #4301) and are reused here, never redeclared, exactly as
	// the VPN Tunnel extractor reuses AssetTypeComputeVPNGateway and
	// AssetTypeComputeRouter from their own owning extractors.
	AssetTypeComputeInterconnect = "compute.googleapis.com/Interconnect"
)

// Bounded provider relationship types for InterconnectAttachment edges,
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly.
const (
	RelationshipTypeInterconnectAttachmentUsesRouter       = "interconnect_attachment_uses_router"
	RelationshipTypeInterconnectAttachmentUsesInterconnect = "interconnect_attachment_uses_interconnect"
	RelationshipTypeInterconnectAttachmentUsesNetwork      = "interconnect_attachment_uses_network"
)

// Compute VPN Gateway constants consolidated from extractor_vpn_gateway.go (#6696).
const (
	// AssetTypeComputeVPNGateway is the CAI asset type for a Cloud HA-VPN gateway
	// (the `vpnGateways` REST collection). It is a regional-only asset type — GCP
	// exposes no global VpnGateway variant, unlike ForwardingRule/Address — so
	// this extractor registers exactly one asset type. It is distinct from
	// AssetTypeComputeTargetVPNGateway (consolidated from extractor_forwarding_rule.go into this leaf),
	// which is the older Classic VPN target-gateway resource that a
	// ForwardingRule.target can reference.
	AssetTypeComputeVPNGateway = "compute.googleapis.com/VpnGateway"
	// RelationshipTypeVPNGatewayInNetwork is the bounded provider relationship
	// type for the VpnGateway -> VPC Network edge. It is a stable string carried
	// on gcp_cloud_relationship facts; the reducer materializes an edge only when
	// both endpoints resolve exactly.
	RelationshipTypeVPNGatewayInNetwork = "vpn_gateway_in_network"
)

// Compute VPN Tunnel constants consolidated from extractor_vpn_tunnel.go (#6696).
const (
	// Asset type constants for the compute VpnTunnel typed-depth extractor and the
	// relationship endpoints it derives. AssetTypeComputeVpnTunnel lives in this leaf (consolidated from extractor_route.go), which reuses it for its own
	// next-hop-VPN-tunnel edge, and is reused here as this extractor's own asset
	// type. AssetTypeComputeTargetVPNGateway lives in this leaf (consolidated from extractor_forwarding_rule.go) and is reused here for
	// the Classic VPN target-gateway edge. AssetTypeComputeRouter lives in this leaf (consolidated from extractor_router.go, #4301) and is reused here for the BGP-dynamic-routing router edge; the owning extractor previously declared
	// its own local copy pending that extractor's merge, per the dedup-pass note
	// this comment used to carry, and that duplicate has now been removed.
	//
	// AssetTypeComputeVPNGateway lives in this leaf (consolidated from extractor_vpn_gateway.go, #4302) and is reused here for the HA
	// VPN peer-gateway edge; the owning extractor previously declared its own local copy
	// pending that extractor's merge, and that duplicate has now been removed.
	AssetTypeComputeExternalVPNGateway = "compute.googleapis.com/ExternalVpnGateway"
)

// Bounded provider relationship types for VpnTunnel edges, carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
//
//   - RelationshipTypeVpnTunnelUsesVpnGateway: HA VPN tunnel -> its own HA VPN
//     gateway (`vpnGateway`).
//   - RelationshipTypeVpnTunnelUsesTargetVpnGateway: Classic VPN tunnel -> its
//     own Classic target VPN gateway (`targetVpnGateway`).
//   - RelationshipTypeVpnTunnelPeersWithVpnGateway: either tunnel kind -> the
//     peer gateway, whether it is a GCP HA peer (`peerGcpGateway`) or an
//     external peer gateway resource (`peerExternalGateway`). Both peer forms
//     share one relationship type because the graph question ("what does this
//     tunnel peer with") is the same regardless of which side owns the peer
//     resource; the target's own asset type distinguishes an
//     `ExternalVpnGateway` peer from an HA `VpnGateway` peer-to-peer topology.
//   - RelationshipTypeVpnTunnelUsesRouter: either tunnel kind -> the Cloud
//     Router used for BGP dynamic routing, present only when the tunnel is
//     configured for dynamic (not policy-based) routing.
const (
	RelationshipTypeVpnTunnelUsesVpnGateway       = "vpn_tunnel_uses_vpn_gateway"
	RelationshipTypeVpnTunnelUsesTargetVpnGateway = "vpn_tunnel_uses_target_vpn_gateway"
	RelationshipTypeVpnTunnelPeersWithVpnGateway  = "vpn_tunnel_peers_with_vpn_gateway"
	RelationshipTypeVpnTunnelUsesRouter           = "vpn_tunnel_uses_router"
)
