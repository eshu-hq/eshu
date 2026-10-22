// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_compute_net.go (#6696 HK slice); uses below name catalog.X.

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
	RegisterAssetExtractor(catalog.AssetTypeComputeVpnTunnel, extractVpnTunnel)
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
		rels = append(rels, vpnTunnelEdge(ctx, catalog.RelationshipTypeVpnTunnelUsesVpnGateway, gatewayName, catalog.AssetTypeComputeVPNGateway))
	}
	if targetGatewayName := computeResourceFullNameFromSelfLink(data.TargetVPNGateway, "targetVpnGateways", ctx.ProjectID); targetGatewayName != "" {
		anchors = append(anchors, targetGatewayName)
		rels = append(rels, vpnTunnelEdge(ctx, catalog.RelationshipTypeVpnTunnelUsesTargetVpnGateway, targetGatewayName, catalog.AssetTypeComputeTargetVPNGateway))
	}
	if peerName, peerType := vpnTunnelPeerGateway(data, ctx.ProjectID); peerName != "" {
		anchors = append(anchors, peerName)
		rels = append(rels, vpnTunnelEdge(ctx, catalog.RelationshipTypeVpnTunnelPeersWithVpnGateway, peerName, peerType))
	}
	if routerName := computeResourceFullNameFromSelfLink(data.Router, "routers", ctx.ProjectID); routerName != "" {
		anchors = append(anchors, routerName)
		rels = append(rels, vpnTunnelEdge(ctx, catalog.RelationshipTypeVpnTunnelUsesRouter, routerName, catalog.AssetTypeComputeRouter))
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
		return name, catalog.AssetTypeComputeVPNGateway
	}
	if name := computeResourceFullNameFromSelfLink(data.PeerExternalGateway, "externalVpnGateways", sourceProjectID); name != "" {
		return name, catalog.AssetTypeComputeExternalVPNGateway
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
