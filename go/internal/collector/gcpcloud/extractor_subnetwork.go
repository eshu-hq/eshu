// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Asset type constants for the compute Subnetwork typed-depth extractor and the
// relationship endpoint it derives. The target asset type names the CAI asset
// type of the parent-network edge so the reducer can resolve both endpoints
// exactly before materializing.
const (
	assetTypeComputeSubnetwork = "compute.googleapis.com/Subnetwork"
	assetTypeComputeNetwork    = "compute.googleapis.com/Network"
)

// relationshipTypeSubnetworkInNetwork is the bounded provider relationship type
// for the subnet -> parent VPC network edge carried on a gcp_cloud_relationship
// fact. The reducer materializes the edge only when both endpoints resolve
// exactly.
const relationshipTypeSubnetworkInNetwork = "subnetwork_in_network"

// computeResourceNamePrefix is the Cloud Asset Inventory full-resource-name
// prefix for compute Engine resources, used to build the parent-network edge
// endpoint from a subnet's network self-link.
const computeResourceNamePrefix = "//compute.googleapis.com/"

func init() {
	RegisterAssetExtractor(assetTypeComputeSubnetwork, extractSubnetwork)
}

// subnetworkData is the bounded view of a CAI compute.googleapis.com/Subnetwork
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded. The address-bearing fields (ipCidrRange,
// gatewayAddress, the secondary range CIDRs, and every IPv6 range/prefix) are
// decoded only so they can be reduced to a non-address signal (a prefix length)
// or dropped; their raw values never leave this extractor, per the GCP collector
// contract Payload Boundaries (no public or private IP addresses persisted).
type subnetworkData struct {
	Network               string `json:"network"`
	Region                string `json:"region"`
	IPCidrRange           string `json:"ipCidrRange"`
	GatewayAddress        string `json:"gatewayAddress"`
	PrivateIPGoogleAccess *bool  `json:"privateIpGoogleAccess"`
	Purpose               string `json:"purpose"`
	Role                  string `json:"role"`
	StackType             string `json:"stackType"`
	EnableFlowLogs        *bool  `json:"enableFlowLogs"`
	CreationTimestamp     string `json:"creationTimestamp"`
	SecondaryIPRanges     []struct {
		RangeName   string `json:"rangeName"`
		IPCidrRange string `json:"ipCidrRange"`
	} `json:"secondaryIpRanges"`
}

// extractSubnetwork extracts bounded, redaction-safe typed depth for one compute
// Subnetwork CAI asset. It returns the Terraform/drift/monitoring attribute set,
// the parent VPC network as a cross-source correlation anchor, and the typed
// subnetwork_in_network edge. Address-bearing fields are never persisted: the
// primary range is reduced to its prefix length (subnet size, not address), the
// gateway and IPv6 ranges are dropped, and secondary ranges are kept only by
// operator-chosen range name.
func extractSubnetwork(ctx ExtractContext) (AttributeExtraction, error) {
	var data subnetworkData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode subnetwork data: %w", err)
	}

	attrs := subnetworkAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if networkName := computeNetworkFullName(data.Network); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, subnetworkEdge(ctx, relationshipTypeSubnetworkInNetwork, networkName, assetTypeComputeNetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// subnetworkAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example an empty purpose or a false
// flow-logs flag that was simply not reported).
func subnetworkAttributes(data subnetworkData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.Purpose); v != "" {
		attrs["purpose"] = v
	}
	if v := strings.TrimSpace(data.Role); v != "" {
		attrs["role"] = v
	}
	if data.PrivateIPGoogleAccess != nil {
		attrs["private_ip_google_access"] = *data.PrivateIPGoogleAccess
	}
	if v := strings.TrimSpace(data.StackType); v != "" {
		attrs["stack_type"] = v
	}
	if data.EnableFlowLogs != nil {
		attrs["enable_flow_logs"] = *data.EnableFlowLogs
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	// Primary range: keep only the prefix length (subnet size), never the address.
	if prefix, ok := cidrPrefixLength(data.IPCidrRange); ok {
		attrs["ip_cidr_prefix_length"] = prefix
	}
	// Secondary ranges: keep operator-chosen names and a count; drop the CIDRs.
	if names := secondaryRangeNames(data); len(names) > 0 {
		attrs["secondary_range_count"] = len(names)
		attrs["secondary_range_names"] = names
	}
	return attrs
}

// secondaryRangeNames returns the deduplicated, non-empty operator-chosen range
// names for a subnet's secondary ranges. The range CIDR values are intentionally
// discarded; only the names (control-plane labels, not addresses) are kept.
func secondaryRangeNames(data subnetworkData) []string {
	if len(data.SecondaryIPRanges) == 0 {
		return nil
	}
	names := make([]string, 0, len(data.SecondaryIPRanges))
	for _, r := range data.SecondaryIPRanges {
		names = append(names, r.RangeName)
	}
	return dedupeNonEmpty(names)
}

// computeNetworkFullName derives the parent network CAI full resource name from a
// subnet's network reference, which CAI may report as a full compute self-link
// or a partial resource path. It returns "" when the reference is blank or does
// not name a network, so the caller emits no parent-network edge.
func computeNetworkFullName(networkRef string) string {
	trimmed := strings.TrimSpace(networkRef)
	if trimmed == "" {
		return ""
	}
	idx := strings.Index(trimmed, "projects/")
	if idx < 0 {
		return ""
	}
	path := trimmed[idx:]
	if !strings.Contains(path, "/networks/") {
		return ""
	}
	return computeResourceNamePrefix + path
}

// computeRegionName extracts the bare region name from a subnet's region
// reference, which CAI may report as a compute self-link, a partial path, or the
// bare region name itself.
func computeRegionName(regionRef string) string {
	trimmed := strings.TrimSpace(regionRef)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/regions/"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+len("/regions/"):])
	}
	if strings.Contains(trimmed, "/") {
		// A path that does not name a region segment is not a bare region name.
		return ""
	}
	return trimmed
}

// cidrPrefixLength returns the prefix length (the number after the slash) of an
// IPv4 or IPv6 CIDR, conveying subnet size without persisting any address. A
// value without a slash or with a non-numeric suffix yields ok=false so the
// attribute is omitted rather than fabricated.
func cidrPrefixLength(cidr string) (int64, bool) {
	trimmed := strings.TrimSpace(cidr)
	_, suffix, found := strings.Cut(trimmed, "/")
	if !found {
		return 0, false
	}
	prefix, err := strconv.ParseInt(strings.TrimSpace(suffix), 10, 64)
	if err != nil {
		return 0, false
	}
	return prefix, true
}

// normalizeRFC3339 parses a compute API RFC3339 timestamp (which carries a zone
// offset and optional fractional seconds) and returns it as an RFC3339 UTC
// string. A blank or unparseable value yields ok=false so the attribute is
// omitted rather than written as the zero time.
func normalizeRFC3339(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339), true
}

func subnetworkEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
