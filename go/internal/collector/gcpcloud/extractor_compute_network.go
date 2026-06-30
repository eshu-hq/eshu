// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the VPC Network typed-depth extractor and the
// relationship endpoints it derives. Target asset types name the CAI asset type
// of each typed edge so reducers can resolve both endpoints exactly.
const (
	assetTypeComputeNetwork    = "compute.googleapis.com/Network"
	assetTypeComputeSubnetwork = "compute.googleapis.com/Subnetwork"
)

// Bounded relationship types for VPC Network edges. They are stable provider
// relationship strings carried on gcp_cloud_relationship facts; the reducer
// materializes an edge only when both endpoints resolve exactly. Only edges
// derivable from the Network resource.data itself are emitted here: contained
// subnetworks and VPC peerings. Firewall, route, and instance attachments
// reference the network from their own resource.data and are emitted by those
// asset types' own extractors, not this one.
const (
	relationshipTypeNetworkContainsSubnetwork = "vpc_network_contains_subnetwork"
	relationshipTypeNetworkPeersWithNetwork   = "vpc_network_peers_with_network"

	// computeResourceNamePrefix is the Cloud Asset Inventory full-resource-name
	// prefix for Compute Engine resources.
	computeResourceNamePrefix = "//compute.googleapis.com/"
	// computeSelfLinkMarker delimits the API version segment in a Compute Engine
	// selfLink; the path after the version segment is the CAI resource path.
	computeSelfLinkMarker = "/compute/"
)

// computeNetworkData is the bounded view of a CAI compute.googleapis.com/Network
// resource.data blob. Only safe control-plane metadata and resource identifiers
// are decoded; data-plane fields (legacy IPv4Range, gatewayIPv4, peering
// public-IP export flags, and similar) are intentionally not decoded and never
// reach the extraction output. AutoCreateSubnetworks is a pointer so an absent
// field is distinguished from an explicit false.
type computeNetworkData struct {
	AutoCreateSubnetworks *bool  `json:"autoCreateSubnetworks"`
	MTU                   int64  `json:"mtu"`
	CreationTimestamp     string `json:"creationTimestamp"`
	RoutingConfig         *struct {
		RoutingMode string `json:"routingMode"`
	} `json:"routingConfig"`
	Subnetworks []string `json:"subnetworks"`
	Peerings    []struct {
		Name    string `json:"name"`
		Network string `json:"network"`
		State   string `json:"state"`
	} `json:"peerings"`
}

func init() {
	RegisterAssetExtractor(assetTypeComputeNetwork, extractComputeNetwork)
}

// extractComputeNetwork extracts bounded typed depth for one VPC Network CAI
// asset. It returns the Terraform/drift/monitoring attribute set (auto-mode flag,
// routing mode, MTU, creation timestamp, and subnetwork/peering counts),
// cross-source correlation anchors (contained subnetwork and peer network full
// resource names), and typed contained-subnetwork and peering relationships. IP
// ranges, gateway IPs, and peering data-plane flags are never decoded so no
// data-plane locator leaves the parser.
func extractComputeNetwork(ctx ExtractContext) (AttributeExtraction, error) {
	var data computeNetworkData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode compute network data: %w", err)
	}

	attrs := computeNetworkAttributes(data)
	anchors := make([]string, 0, len(data.Subnetworks)+len(data.Peerings))
	rels := make([]RelationshipObservation, 0, len(data.Subnetworks)+len(data.Peerings))

	for _, link := range data.Subnetworks {
		name := computeFullResourceNameFromSelfLink(link)
		if name == "" {
			continue
		}
		anchors = append(anchors, name)
		rels = append(rels, computeNetworkEdge(ctx, relationshipTypeNetworkContainsSubnetwork, name, assetTypeComputeSubnetwork))
	}

	for _, peering := range data.Peerings {
		name := computeFullResourceNameFromSelfLink(peering.Network)
		if name == "" || name == strings.TrimSpace(ctx.FullResourceName) {
			continue
		}
		anchors = append(anchors, name)
		rels = append(rels, computeNetworkEdge(ctx, relationshipTypeNetworkPeersWithNetwork, name, assetTypeComputeNetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// computeNetworkAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate an MTU of 0 or an empty routing mode. Counts are omitted
// when their list is empty so an auto-mode network with no listed subnetworks
// does not report a fabricated "0 subnetworks".
func computeNetworkAttributes(data computeNetworkData) map[string]any {
	attrs := map[string]any{}
	if data.AutoCreateSubnetworks != nil {
		attrs["auto_create_subnetworks"] = *data.AutoCreateSubnetworks
	}
	if data.RoutingConfig != nil {
		if mode := strings.TrimSpace(data.RoutingConfig.RoutingMode); mode != "" {
			attrs["routing_mode"] = mode
		}
	}
	if data.MTU > 0 {
		attrs["mtu"] = data.MTU
	}
	if ts := strings.TrimSpace(data.CreationTimestamp); ts != "" {
		attrs["creation_timestamp"] = ts
	}
	if n := len(data.Subnetworks); n > 0 {
		attrs["subnetwork_count"] = n
	}
	if n := len(data.Peerings); n > 0 {
		attrs["peering_count"] = n
	}
	return attrs
}

// computeFullResourceNameFromSelfLink converts a Compute Engine selfLink (or a
// peering network URL) into its Cloud Asset Inventory full resource name. It
// returns an empty string for a blank link, a link with no recognizable
// /compute/<version>/projects/... path, or a link that is already malformed, so
// only well-formed resource identities become edge endpoints. A value already in
// CAI full-resource-name form is returned unchanged.
func computeFullResourceNameFromSelfLink(selfLink string) string {
	trimmed := strings.TrimSpace(selfLink)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, computeResourceNamePrefix) {
		return trimmed
	}
	idx := strings.Index(trimmed, computeSelfLinkMarker)
	if idx < 0 {
		return ""
	}
	afterMarker := trimmed[idx+len(computeSelfLinkMarker):]
	// afterMarker is "<version>/projects/...": drop the version segment.
	_, path, ok := strings.Cut(afterMarker, "/")
	if !ok {
		return ""
	}
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "projects/") {
		return ""
	}
	return computeResourceNamePrefix + path
}

func computeNetworkEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
