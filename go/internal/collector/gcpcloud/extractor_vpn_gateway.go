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

func init() {
	RegisterAssetExtractor(catalog.AssetTypeComputeVPNGateway, extractVPNGateway)
}

// vpnGatewayData is the bounded view of a CAI compute.googleapis.com/VpnGateway
// resource.data blob. Only safe control-plane metadata and resource
// identifiers are decoded. vpnInterfaces carries per-interface identity and
// address fields in the CAI payload, but only the interface count is kept:
// per the GCP collector contract Payload Boundaries, no public or private IP
// address (ipAddress, ipv6Address), no interconnect-attachment resource
// reference, and no per-interface id crosses into Go memory. VPNInterfaces is
// declared as []struct{} rather than a struct with an id field because
// encoding/json silently ignores JSON object keys with no matching struct
// field, so these values are never decoded at all, not just never emitted,
// mirroring the ForwardingRule/Address extractors' treatment of their own
// reserved address fields.
type vpnGatewayData struct {
	Region            string     `json:"region"`
	Network           string     `json:"network"`
	StackType         string     `json:"stackType"`
	GatewayIPVersion  string     `json:"gatewayIpVersion"`
	CreationTimestamp string     `json:"creationTimestamp"`
	VPNInterfaces     []struct{} `json:"vpnInterfaces"`
}

// extractVPNGateway extracts bounded, redaction-safe typed depth for one
// Cloud HA-VPN Gateway CAI asset. It returns the Terraform/drift/monitoring
// attribute set (region, stack type, gateway IP version, creation time, and a
// bounded VPN-interface count), the enclosing VPC network as a correlation
// anchor, and the typed network edge. Per-interface identity (id), the
// interface IP/IPv6 address, and any interconnect-attachment reference are
// never decoded — only the interface count crosses the redaction boundary, so
// no public or private IP address reaches a fact.
func extractVPNGateway(ctx ExtractContext) (AttributeExtraction, error) {
	var data vpnGatewayData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode vpn gateway data: %w", err)
	}

	attrs := vpnGatewayAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, vpnGatewayEdge(ctx, catalog.RelationshipTypeVPNGatewayInNetwork, networkName, assetTypeComputeNetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// vpnGatewayAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a stack type or a fabricated "0 interfaces" count for a
// gateway whose vpnInterfaces list was not returned.
func vpnGatewayAttributes(data vpnGatewayData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.StackType); v != "" {
		attrs["stack_type"] = v
	}
	if v := strings.TrimSpace(data.GatewayIPVersion); v != "" {
		attrs["gateway_ip_version"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	if n := len(data.VPNInterfaces); n > 0 {
		attrs["vpn_interface_count"] = n
	}
	return attrs
}

func vpnGatewayEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
