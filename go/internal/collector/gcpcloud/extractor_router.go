// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute Router typed-depth extractor and the
// relationship endpoints it derives. assetTypeComputeNetwork,
// assetTypeComputeSubnetwork, and assetTypeComputeVpnTunnel are declared by
// the sibling Network/Subnetwork and Route extractors and reused here.
// assetTypeComputeInterconnectAttachment is declared here since no sibling
// extractor for that asset type exists yet.
const (
	assetTypeComputeRouter                 = "compute.googleapis.com/Router"
	assetTypeComputeInterconnectAttachment = "compute.googleapis.com/InterconnectAttachment"
)

// Bounded provider relationship types for Router edges carried on
// gcp_cloud_relationship facts. The reducer materializes an edge only when
// both endpoints resolve exactly. BGP peers themselves never become edge
// endpoints: a peer names an interface (`interfaceName`), and only the
// interface's own linked resource (a VPN tunnel, an Interconnect attachment,
// or a subnetwork) is a resolvable CAI asset.
const (
	relationshipTypeRouterInNetwork                             = "router_in_network"
	relationshipTypeRouterInterfaceLinkedVpnTunnel              = "router_interface_linked_vpn_tunnel"
	relationshipTypeRouterInterfaceLinkedInterconnectAttachment = "router_interface_linked_interconnect_attachment"
	relationshipTypeRouterInterfaceSubnetwork                   = "router_interface_subnetwork"
)

func init() {
	RegisterAssetExtractor(assetTypeComputeRouter, extractRouter)
}

// routerData is the bounded view of a CAI compute.googleapis.com/Router
// resource.data blob. EncryptedInterconnectRouter is a pointer so an absent
// field is distinguished from an explicit false. Router interface IP ranges
// (routerInterfaceData.IPRange) and BGP peer/interface IP addresses
// (routerBgpPeerData.IPAddress, PeerIPAddress) are intentionally never
// decoded here, per the GCP collector contract Payload Boundaries: only
// resource identities (names, ASNs, linked-resource references) leave this
// extractor.
type routerData struct {
	Region                      string                `json:"region"`
	Network                     string                `json:"network"`
	EncryptedInterconnectRouter *bool                 `json:"encryptedInterconnectRouter"`
	CreationTimestamp           string                `json:"creationTimestamp"`
	Bgp                         *routerBgpData        `json:"bgp"`
	BgpPeers                    []routerBgpPeerData   `json:"bgpPeers"`
	Nats                        []routerNatData       `json:"nats"`
	Interfaces                  []routerInterfaceData `json:"interfaces"`
}

// routerBgpData is the bounded view of a Router's router-level BGP
// configuration. AdvertisedGroups/AdvertisedIpRanges are never decoded since
// they carry advertised CIDR data.
type routerBgpData struct {
	Asn           int64  `json:"asn"`
	AdvertiseMode string `json:"advertiseMode"`
}

// routerBgpPeerData is the bounded view of one Router BGP peer entry.
// IPAddress and PeerIPAddress are declared only so json.Unmarshal does not
// error on their presence; they are never read by any attribute builder in
// this file, so no BGP peer address reaches the extraction output.
type routerBgpPeerData struct {
	Name          string `json:"name"`
	PeerAsn       int64  `json:"peerAsn"`
	InterfaceName string `json:"interfaceName"`
	IPAddress     string `json:"ipAddress"`
	PeerIPAddress string `json:"peerIpAddress"`
}

// routerNatData is the bounded view of one Cloud NAT service configured on a
// Router. NatIps and DrainNatIps are declared only so json.Unmarshal does not
// error on their presence; neither is read by any attribute builder in this
// file, so no NAT IP resource reference reaches the extraction output.
type routerNatData struct {
	Name                          string   `json:"name"`
	NatIpAllocateOption           string   `json:"natIpAllocateOption"`
	SourceSubnetworkIpRangesToNat string   `json:"sourceSubnetworkIpRangesToNat"`
	NatIps                        []string `json:"natIps"`
	DrainNatIps                   []string `json:"drainNatIps"`
}

// routerInterfaceData is the bounded view of one Router interface entry.
// IPRange is declared only so json.Unmarshal does not error on its presence;
// it is never read by any attribute builder in this file, so no interface IP
// range reaches the extraction output. Each interface names at most one
// linked resource (a VPN tunnel, an Interconnect attachment, or a
// subnetwork); LinkedVpnTunnel, LinkedInterconnectAttachment, and Subnetwork
// resolve to independent edges when present.
type routerInterfaceData struct {
	Name                         string `json:"name"`
	IPRange                      string `json:"ipRange"`
	LinkedVpnTunnel              string `json:"linkedVpnTunnel"`
	LinkedInterconnectAttachment string `json:"linkedInterconnectAttachment"`
	Subnetwork                   string `json:"subnetwork"`
}

// extractRouter extracts bounded, redaction-safe typed depth for one compute
// Router CAI asset. It returns the Terraform/drift/monitoring attribute set
// (region, BGP ASN/advertise mode, a bounded per-peer summary of name/peer
// ASN/interface name, a bounded per-NAT summary of name/IP-allocate-option/
// source-subnetwork-ranges, encrypted-interconnect-router posture, and
// creation time); the enclosing network as a correlation anchor and edge; and
// typed interface edges to each interface's linked VPN tunnel, linked
// Interconnect attachment, or subnetwork. No BGP peer or interface IP
// address, and no NAT IP resource reference, ever reaches the output.
func extractRouter(ctx ExtractContext) (AttributeExtraction, error) {
	var data routerData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode router data: %w", err)
	}

	attrs := routerAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, routerEdge(ctx, relationshipTypeRouterInNetwork, networkName, assetTypeComputeNetwork))
	}

	for _, iface := range data.Interfaces {
		if tunnelName := computeFullResourceNameFromSelfLink(iface.LinkedVpnTunnel, ctx.ProjectID); tunnelName != "" {
			anchors = append(anchors, tunnelName)
			rels = append(rels, routerEdge(ctx, relationshipTypeRouterInterfaceLinkedVpnTunnel, tunnelName, assetTypeComputeVpnTunnel))
		}
		if attachmentName := computeFullResourceNameFromSelfLink(iface.LinkedInterconnectAttachment, ctx.ProjectID); attachmentName != "" {
			anchors = append(anchors, attachmentName)
			rels = append(rels, routerEdge(ctx, relationshipTypeRouterInterfaceLinkedInterconnectAttachment, attachmentName, assetTypeComputeInterconnectAttachment))
		}
		if subnetName := computeFullResourceNameFromSelfLink(iface.Subnetwork, ctx.ProjectID); subnetName != "" {
			anchors = append(anchors, subnetName)
			rels = append(rels, routerEdge(ctx, relationshipTypeRouterInterfaceSubnetwork, subnetName, assetTypeComputeSubnetwork))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// routerAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example a false
// encrypted_interconnect_router). Peer and NAT counts are omitted when their
// list is empty so a router with no peers/NATs does not report a fabricated
// zero count.
func routerAttributes(data routerData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if data.Bgp != nil {
		if data.Bgp.Asn != 0 {
			attrs["bgp_asn"] = data.Bgp.Asn
		}
		if v := strings.TrimSpace(data.Bgp.AdvertiseMode); v != "" {
			attrs["bgp_advertise_mode"] = v
		}
	}
	if peers := routerBgpPeerSummaries(data.BgpPeers); len(peers) > 0 {
		attrs["bgp_peers"] = peers
		attrs["bgp_peer_count"] = len(peers)
	}
	if nats := routerNatSummaries(data.Nats); len(nats) > 0 {
		attrs["nats"] = nats
		attrs["nat_count"] = len(nats)
	}
	if n := len(data.Interfaces); n > 0 {
		attrs["interface_count"] = n
	}
	if data.EncryptedInterconnectRouter != nil {
		attrs["encrypted_interconnect_router"] = *data.EncryptedInterconnectRouter
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// routerBgpPeerSummaries builds the bounded per-peer summary list: name, peer
// ASN, and interface name only. Neither ipAddress nor peerIpAddress is ever
// read, so no BGP peer address reaches the summary.
func routerBgpPeerSummaries(peers []routerBgpPeerData) []map[string]any {
	if len(peers) == 0 {
		return nil
	}
	summaries := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		summary := map[string]any{}
		if v := strings.TrimSpace(peer.Name); v != "" {
			summary["name"] = v
		}
		if peer.PeerAsn != 0 {
			summary["peer_asn"] = peer.PeerAsn
		}
		if v := strings.TrimSpace(peer.InterfaceName); v != "" {
			summary["interface_name"] = v
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// routerNatSummaries builds the bounded per-NAT summary list: name, IP
// allocation option, and source-subnetwork-ranges option only. Neither natIps
// nor drainNatIps is ever read, so no NAT IP resource reference reaches the
// summary.
func routerNatSummaries(nats []routerNatData) []map[string]any {
	if len(nats) == 0 {
		return nil
	}
	summaries := make([]map[string]any, 0, len(nats))
	for _, nat := range nats {
		summary := map[string]any{}
		if v := strings.TrimSpace(nat.Name); v != "" {
			summary["name"] = v
		}
		if v := strings.TrimSpace(nat.NatIpAllocateOption); v != "" {
			summary["nat_ip_allocate_option"] = v
		}
		if v := strings.TrimSpace(nat.SourceSubnetworkIpRangesToNat); v != "" {
			summary["source_subnetwork_ip_ranges_to_nat"] = v
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// routerEdge builds a supported typed relationship observation rooted at the
// router.
func routerEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
