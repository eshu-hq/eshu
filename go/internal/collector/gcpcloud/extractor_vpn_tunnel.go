// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute VpnTunnel typed-depth extractor and the
// relationship endpoints it derives. assetTypeComputeVpnTunnel is declared by
// the sibling Route extractor (extractor_route.go), which reuses it for its own
// next-hop-VPN-tunnel edge, and is reused here as this extractor's own asset
// type. assetTypeComputeTargetVPNGateway is declared by the sibling
// ForwardingRule extractor (extractor_forwarding_rule.go) and reused here for
// the Classic VPN target-gateway edge. assetTypeComputeRouter is declared by
// the sibling Cloud Router extractor (extractor_router.go, #4301) and reused
// here for the BGP-dynamic-routing router edge; this file previously declared
// its own local copy pending that extractor's merge, per the dedup-pass note
// this comment used to carry, and that duplicate has now been removed.
//
// assetTypeComputeVPNGateway (HA VPN gateway) has no existing declaration
// anywhere else in this package as of this extractor: the sibling gcp/B
// ticket for Cloud VPN Gateway (#4302) had not merged when this extractor was
// authored, so this constant is still declared locally following the same
// `compute.googleapis.com/<Type>` naming convention as every other asset-type
// constant in this package. If that sibling PR lands with its own
// declaration of the same constant, a follow-up dedup pass must remove the
// duplicate here and reuse the sibling's declaration, exactly as this file
// already reuses assetTypeComputeVpnTunnel, assetTypeComputeTargetVPNGateway,
// and assetTypeComputeRouter from their own sibling extractors.
const (
	assetTypeComputeVPNGateway         = "compute.googleapis.com/VpnGateway"
	assetTypeComputeExternalVPNGateway = "compute.googleapis.com/ExternalVpnGateway"
)

// Bounded provider relationship types for VpnTunnel edges, carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
//
//   - relationshipTypeVpnTunnelUsesVpnGateway: HA VPN tunnel -> its own HA VPN
//     gateway (`vpnGateway`).
//   - relationshipTypeVpnTunnelUsesTargetVpnGateway: Classic VPN tunnel -> its
//     own Classic target VPN gateway (`targetVpnGateway`).
//   - relationshipTypeVpnTunnelPeersWithVpnGateway: either tunnel kind -> the
//     peer gateway, whether it is a GCP HA peer (`peerGcpGateway`) or an
//     external peer gateway resource (`peerExternalGateway`). Both peer forms
//     share one relationship type because the graph question ("what does this
//     tunnel peer with") is the same regardless of which side owns the peer
//     resource; the target's own asset type distinguishes an
//     `ExternalVpnGateway` peer from an HA `VpnGateway` peer-to-peer topology.
//   - relationshipTypeVpnTunnelUsesRouter: either tunnel kind -> the Cloud
//     Router used for BGP dynamic routing, present only when the tunnel is
//     configured for dynamic (not policy-based) routing.
const (
	relationshipTypeVpnTunnelUsesVpnGateway       = "vpn_tunnel_uses_vpn_gateway"
	relationshipTypeVpnTunnelUsesTargetVpnGateway = "vpn_tunnel_uses_target_vpn_gateway"
	relationshipTypeVpnTunnelPeersWithVpnGateway  = "vpn_tunnel_peers_with_vpn_gateway"
	relationshipTypeVpnTunnelUsesRouter           = "vpn_tunnel_uses_router"
)

// vpnTunnelData is the bounded view of a CAI compute.googleapis.com/VpnTunnel
// resource.data blob. PeerIP, SharedSecret, SharedSecretHash, and
// DetailedStatus are intentionally NOT decoded fields: per the GCP collector
// contract Payload Boundaries and this extractor's own contract, no public or
// private IP address and no pre-shared-key material or free-text status detail
// ever reaches a fact. LocalTrafficSelector and RemoteTrafficSelector are
// decoded only to derive bounded counts; their CIDR values are never persisted,
// mirroring the Route extractor's destRange-to-prefix-length reduction and the
// Subnetwork extractor's range-to-prefix-length reduction.
type vpnTunnelData struct {
	Region                       string   `json:"region"`
	VPNGateway                   string   `json:"vpnGateway"`
	VPNGatewayInterface          *int64   `json:"vpnGatewayInterface"`
	TargetVPNGateway             string   `json:"targetVpnGateway"`
	PeerExternalGateway          string   `json:"peerExternalGateway"`
	PeerExternalGatewayInterface *int64   `json:"peerExternalGatewayInterface"`
	PeerGCPGateway               string   `json:"peerGcpGateway"`
	Router                       string   `json:"router"`
	IKEVersion                   *int64   `json:"ikeVersion"`
	Status                       string   `json:"status"`
	LocalTrafficSelector         []string `json:"localTrafficSelector"`
	RemoteTrafficSelector        []string `json:"remoteTrafficSelector"`
	CreationTimestamp            string   `json:"creationTimestamp"`
}

func init() {
	RegisterAssetExtractor(assetTypeComputeVpnTunnel, extractVpnTunnel)
}

// extractVpnTunnel extracts bounded, redaction-safe typed depth for one compute
// VpnTunnel CAI asset (both Classic and HA VPN tunnels share this resource
// type). It returns the Terraform/drift/monitoring attribute set (region, IKE
// version, tunnel status, gateway-interface indexes, and bounded
// traffic-selector counts); cross-source correlation anchors for the resolvable
// gateway/peer/router references; and the typed edges to those resources. The
// tunnel's own `peerIp`, `sharedSecret`, `sharedSecretHash`, and
// `detailedStatus` fields are never decoded, so no address, key material, or
// free-text status detail reaches a fact.
func extractVpnTunnel(ctx ExtractContext) (AttributeExtraction, error) {
	var data vpnTunnelData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode vpn tunnel data: %w", err)
	}

	attrs := vpnTunnelAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if gatewayName := computeResourceFullNameFromSelfLink(data.VPNGateway, "vpnGateways", ctx.ProjectID); gatewayName != "" {
		anchors = append(anchors, gatewayName)
		rels = append(rels, vpnTunnelEdge(ctx, relationshipTypeVpnTunnelUsesVpnGateway, gatewayName, assetTypeComputeVPNGateway))
	}
	if targetGatewayName := computeResourceFullNameFromSelfLink(data.TargetVPNGateway, "targetVpnGateways", ctx.ProjectID); targetGatewayName != "" {
		anchors = append(anchors, targetGatewayName)
		rels = append(rels, vpnTunnelEdge(ctx, relationshipTypeVpnTunnelUsesTargetVpnGateway, targetGatewayName, assetTypeComputeTargetVPNGateway))
	}
	if peerName, peerType := vpnTunnelPeerGateway(data, ctx.ProjectID); peerName != "" {
		anchors = append(anchors, peerName)
		rels = append(rels, vpnTunnelEdge(ctx, relationshipTypeVpnTunnelPeersWithVpnGateway, peerName, peerType))
	}
	if routerName := computeResourceFullNameFromSelfLink(data.Router, "routers", ctx.ProjectID); routerName != "" {
		anchors = append(anchors, routerName)
		rels = append(rels, vpnTunnelEdge(ctx, relationshipTypeVpnTunnelUsesRouter, routerName, assetTypeComputeRouter))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// vpnTunnelAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture. Traffic selectors are reduced to counts; no
// CIDR value is ever persisted.
func vpnTunnelAttributes(data vpnTunnelData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if data.VPNGatewayInterface != nil {
		attrs["vpn_gateway_interface"] = *data.VPNGatewayInterface
	}
	if data.PeerExternalGatewayInterface != nil {
		attrs["peer_external_gateway_interface"] = *data.PeerExternalGatewayInterface
	}
	if data.IKEVersion != nil {
		attrs["ike_version"] = *data.IKEVersion
	}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if n := len(data.LocalTrafficSelector); n > 0 {
		attrs["local_traffic_selector_count"] = int64(n)
	}
	if n := len(data.RemoteTrafficSelector); n > 0 {
		attrs["remote_traffic_selector_count"] = int64(n)
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// vpnTunnelPeerGateway resolves a VpnTunnel's peer gateway reference to its CAI
// full resource name and asset type. peerGcpGateway (HA VPN peer-to-peer) and
// peerExternalGateway (Classic or HA VPN peer to an external gateway resource)
// are mutually exclusive per the compute API contract; peerGcpGateway is
// checked first since it is only ever set together with vpnGateway (HA-only),
// matching the API's own precedence. Each field is verified against its own
// expected resource-path segment (vpnGateways for peerGcpGateway,
// externalVpnGateways for peerExternalGateway) via
// computeResourceFullNameFromSelfLink, so a resolvable-but-wrong-kind
// reference in either field (for example an externalVpnGateways selfLink
// placed in peerGcpGateway) never resolves. It returns a blank name for an
// empty, unrecognized, wrong-segment, or otherwise unresolvable reference, so
// the caller emits no edge and no anchor for an ambiguous or mistyped peer.
func vpnTunnelPeerGateway(data vpnTunnelData, sourceProjectID string) (fullName, assetType string) {
	if name := computeResourceFullNameFromSelfLink(data.PeerGCPGateway, "vpnGateways", sourceProjectID); name != "" {
		return name, assetTypeComputeVPNGateway
	}
	if name := computeResourceFullNameFromSelfLink(data.PeerExternalGateway, "externalVpnGateways", sourceProjectID); name != "" {
		return name, assetTypeComputeExternalVPNGateway
	}
	return "", ""
}

// vpnTunnelEdge builds a supported typed relationship observation rooted at the
// VPN tunnel.
func vpnTunnelEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
