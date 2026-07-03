// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeComputeVPNGateway is the CAI asset type for a Cloud HA-VPN gateway
// (the `vpnGateways` REST collection). It is a regional-only asset type — GCP
// exposes no global VpnGateway variant, unlike ForwardingRule/Address — so
// this extractor registers exactly one asset type. It is distinct from
// assetTypeComputeTargetVPNGateway (declared in extractor_forwarding_rule.go),
// which is the older Classic VPN target-gateway resource that a
// ForwardingRule.target can reference.
const assetTypeComputeVPNGateway = "compute.googleapis.com/VpnGateway"

// relationshipTypeVPNGatewayInNetwork is the bounded provider relationship
// type for the VpnGateway -> VPC Network edge. It is a stable string carried
// on gcp_cloud_relationship facts; the reducer materializes an edge only when
// both endpoints resolve exactly.
const relationshipTypeVPNGatewayInNetwork = "vpn_gateway_in_network"

func init() {
	RegisterAssetExtractor(assetTypeComputeVPNGateway, extractVPNGateway)
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
		rels = append(rels, vpnGatewayEdge(ctx, relationshipTypeVPNGatewayInNetwork, networkName, assetTypeComputeNetwork))
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
