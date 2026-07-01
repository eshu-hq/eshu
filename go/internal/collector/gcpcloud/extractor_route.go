// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute Route typed-depth extractor and the
// relationship endpoints it derives. assetTypeComputeNetwork,
// assetTypeComputeInstance, and assetTypeComputeForwardingRule (the next-hop
// internal-load-balancer edge target) are declared by the sibling compute
// extractors and reused here.
const (
	assetTypeComputeRoute     = "compute.googleapis.com/Route"
	assetTypeComputeVpnTunnel = "compute.googleapis.com/VpnTunnel"
)

// Bounded provider relationship types for the Route edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when both
// endpoints resolve exactly. The next-hop gateway (an internet gateway, not a CAI
// asset) and next-hop IP (a data-plane address) are kept as bounded attributes,
// not edges.
const (
	relationshipTypeRouteInNetwork        = "route_in_network"
	relationshipTypeRouteNextHopInstance  = "route_next_hop_instance"
	relationshipTypeRouteNextHopVpnTunnel = "route_next_hop_vpn_tunnel"
	relationshipTypeRouteNextHopIlb       = "route_next_hop_ilb"
)

// routeData is the bounded view of a CAI compute.googleapis.com/Route
// resource.data blob. The destination range is decoded only to derive a
// prefix length and a default-route signal; its address is never persisted. The
// next-hop IP is decoded only to detect its presence; its value is never
// persisted, per the GCP collector contract Payload Boundaries.
type routeData struct {
	Network           string          `json:"network"`
	DestRange         string          `json:"destRange"`
	Priority          json.RawMessage `json:"priority"`
	NextHopGateway    string          `json:"nextHopGateway"`
	NextHopInstance   string          `json:"nextHopInstance"`
	NextHopIP         string          `json:"nextHopIp"`
	NextHopVpnTunnel  string          `json:"nextHopVpnTunnel"`
	NextHopIlb        string          `json:"nextHopIlb"`
	Tags              []string        `json:"tags"`
	CreationTimestamp string          `json:"creationTimestamp"`
}

func init() {
	RegisterAssetExtractor(assetTypeComputeRoute, extractRoute)
}

// extractRoute extracts bounded, redaction-safe typed depth for one compute
// Route CAI asset. It returns the Terraform/drift/monitoring attribute set
// (destination prefix length and default-route flag, priority, next-hop gateway
// leaf name, next-hop-IP presence flag, network tags, creation time); the
// enclosing network and each resolvable next-hop resource (instance, VPN tunnel,
// or internal load balancer) as correlation anchors; and the typed network and
// next-hop edges. No destination CIDR or next-hop IP address reaches a fact.
func extractRoute(ctx ExtractContext) (AttributeExtraction, error) {
	var data routeData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode route data: %w", err)
	}

	attrs := routeAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, routeEdge(ctx, relationshipTypeRouteInNetwork, networkName, assetTypeComputeNetwork))
	}
	if instanceName := routeNextHopName(data.NextHopInstance, ctx.ProjectID, "instances"); instanceName != "" {
		anchors = append(anchors, instanceName)
		rels = append(rels, routeEdge(ctx, relationshipTypeRouteNextHopInstance, instanceName, assetTypeComputeInstance))
	}
	if tunnelName := routeNextHopName(data.NextHopVpnTunnel, ctx.ProjectID, "vpnTunnels"); tunnelName != "" {
		anchors = append(anchors, tunnelName)
		rels = append(rels, routeEdge(ctx, relationshipTypeRouteNextHopVpnTunnel, tunnelName, assetTypeComputeVpnTunnel))
	}
	if ilbName := routeNextHopName(data.NextHopIlb, ctx.ProjectID, "forwardingRules"); ilbName != "" {
		anchors = append(anchors, ilbName)
		rels = append(rels, routeEdge(ctx, relationshipTypeRouteNextHopIlb, ilbName, assetTypeComputeForwardingRule))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// routeAttributes assembles the bounded attribute map. Empty or absent fields are
// omitted rather than written as zero values. The destination range is reduced to
// a prefix length and a default-route boolean; the address is never persisted.
func routeAttributes(data routeData) map[string]any {
	attrs := map[string]any{}
	if prefix, ok := cidrPrefixLength(data.DestRange); ok {
		attrs["dest_prefix_length"] = prefix
		if destRangeIsDefault(data.DestRange) {
			attrs["dest_is_default"] = true
		}
	}
	if v, ok := parseFlexibleInt64(data.Priority); ok {
		attrs["priority"] = v
	}
	if v := gatewayLeafName(data.NextHopGateway); v != "" {
		attrs["next_hop_gateway"] = v
	}
	if strings.TrimSpace(data.NextHopIP) != "" {
		attrs["has_next_hop_ip"] = true
	}
	if tags := dedupeNonEmpty(data.Tags); len(tags) > 0 {
		attrs["network_tags"] = tags
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// destRangeIsDefault reports whether a destination range is the IPv4 or IPv6
// default route (0.0.0.0/0 or ::/0), a public-egress posture signal.
func destRangeIsDefault(destRange string) bool {
	switch strings.TrimSpace(destRange) {
	case "0.0.0.0/0", "::/0":
		return true
	}
	return false
}

// routeNextHopName resolves a Route next-hop reference to its CAI full resource
// name and confirms it names the expected resource segment (instances,
// vpnTunnels, forwardingRules). It uses computeFullResourceNameFromSelfLink so
// the Google-supported project-less forms (regions/r/forwardingRules/fr,
// zones/z/instances/i) resolve against the route's project, and returns "" for a
// bare IP or any reference that does not name the requested segment, so those
// stay unresolvable rather than producing a wrong-typed edge.
func routeNextHopName(ref, projectID, segment string) string {
	name := computeFullResourceNameFromSelfLink(ref, projectID)
	if name == "" || !strings.Contains(name, "/"+segment+"/") {
		return ""
	}
	return name
}

// gatewayLeafName extracts the bare gateway name (for example
// default-internet-gateway) from a next-hop gateway reference. The gateway is not
// a resolvable CAI asset, so its leaf name is kept as an attribute rather than an
// edge endpoint. Only the single leaf segment is returned; any further path
// segments after the gateway name are dropped.
func gatewayLeafName(gatewayRef string) string {
	trimmed := strings.TrimSpace(gatewayRef)
	if trimmed == "" {
		return ""
	}
	leaf := trimmed
	if idx := strings.LastIndex(trimmed, "/gateways/"); idx >= 0 {
		leaf = trimmed[idx+len("/gateways/"):]
	} else if strings.Contains(trimmed, "/") {
		return ""
	}
	// Keep only the first path segment so a trailing suffix cannot leak into the
	// leaf name.
	if slash := strings.Index(leaf, "/"); slash >= 0 {
		leaf = leaf[:slash]
	}
	return strings.TrimSpace(leaf)
}

// routeEdge builds a supported typed relationship observation rooted at the route.
func routeEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
