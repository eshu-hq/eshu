// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bounded relationship types for VPC Network edges. They are stable provider
// relationship strings carried on gcp_cloud_relationship facts; the reducer
// materializes an edge only when both endpoints resolve exactly. Only edges
// derivable from the Network resource.data itself are emitted here: contained
// subnetworks and VPC peerings. Firewall, route, and instance attachments
// reference the network from their own resource.data and are emitted by those
// asset types' own extractors, not this one. The asset type constants
// (assetTypeComputeNetwork, assetTypeComputeSubnetwork) and
// computeResourceNamePrefix are shared with the Subnetwork extractor in
// extractor_subnetwork.go.
const (
	relationshipTypeNetworkContainsSubnetwork = "vpc_network_contains_subnetwork"
	relationshipTypeNetworkPeersWithNetwork   = "vpc_network_peers_with_network"

	// computeSelfLinkMarker delimits the API version segment in a Compute Engine
	// selfLink; the path after the version segment is the CAI resource path.
	computeSelfLinkMarker = "/compute/"
	// peeringStateActive is the Compute Network peering state that exchanges
	// routes/connectivity. Only ACTIVE peerings become materializing edges.
	peeringStateActive = "ACTIVE"
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
		name := computeFullResourceNameFromSelfLink(link, ctx.ProjectID)
		if name == "" {
			continue
		}
		anchors = append(anchors, name)
		rels = append(rels, computeNetworkEdge(ctx, relationshipTypeNetworkContainsSubnetwork, name, assetTypeComputeSubnetwork))
	}

	for _, peering := range data.Peerings {
		// Only ACTIVE peerings exchange routes/connectivity, so only an ACTIVE
		// peering may produce a materializing edge and correlation anchor; an
		// INACTIVE or pending peering would otherwise become a false graph edge
		// once both endpoints resolve. It is still counted in peering_count.
		if !strings.EqualFold(strings.TrimSpace(peering.State), peeringStateActive) {
			continue
		}
		name := computeFullResourceNameFromSelfLink(peering.Network, ctx.ProjectID)
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

// computeFullResourceNameFromSelfLink converts a Compute Engine selfLink or a
// peering network reference into its Cloud Asset Inventory full resource name.
// The Compute API expresses these references as a full selfLink
// (https://.../compute/<version>/projects/...), a project-qualified partial
// (projects/p/...), or a project-less partial (global/networks/n,
// regions/r/subnetworks/s); project-less partials resolve against sourceProjectID
// (the source network's project). A value already in CAI full-resource-name form
// is returned unchanged. An empty string is returned for a blank reference, an
// unrecognized shape, or a project-less partial with no source project, so only
// well-formed resource identities become edge endpoints.
func computeFullResourceNameFromSelfLink(selfLink, sourceProjectID string) string {
	trimmed := strings.TrimSpace(selfLink)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, computeResourceNamePrefix) {
		return trimmed
	}
	// Full selfLink: take the path after the /compute/<version>/ segment.
	if idx := strings.Index(trimmed, computeSelfLinkMarker); idx >= 0 {
		afterMarker := trimmed[idx+len(computeSelfLinkMarker):]
		if _, path, ok := strings.Cut(afterMarker, "/"); ok {
			if normalized := computeResourceNameFromPath(path, sourceProjectID); normalized != "" {
				return normalized
			}
		}
		return ""
	}
	// Partial reference (no scheme, no /compute/ segment).
	return computeResourceNameFromPath(trimmed, sourceProjectID)
}

// computeResourceNameFromPath builds a CAI full resource name from a Compute
// resource path that is either project-qualified (projects/p/...) or a
// project-less partial (global/..., regions/..., zones/...) resolved against
// sourceProjectID. It returns an empty string for any other shape.
func computeResourceNameFromPath(path, sourceProjectID string) string {
	path = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(path), "/"))
	switch {
	case path == "":
		return ""
	case strings.HasPrefix(path, "projects/"):
		return computeResourceNamePrefix + path
	case strings.HasPrefix(path, "global/"),
		strings.HasPrefix(path, "regions/"),
		strings.HasPrefix(path, "zones/"):
		project := strings.TrimSpace(sourceProjectID)
		if project == "" {
			return ""
		}
		return computeResourceNamePrefix + "projects/" + project + "/" + path
	default:
		return ""
	}
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
