// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeComputeInterconnect is the CAI asset type for the underlying
// Dedicated/Cross-Cloud Interconnect a Compute InterconnectAttachment
// traverses. assetTypeComputeInterconnectAttachment and assetTypeComputeRouter
// are declared by the sibling Cloud Router extractor
// (extractor_router.go, #4301) and reused here, never redeclared, exactly as
// the VPN Tunnel extractor reuses assetTypeComputeVPNGateway and
// assetTypeComputeRouter from their own owning extractors.
const assetTypeComputeInterconnect = "compute.googleapis.com/Interconnect"

// Bounded provider relationship types for InterconnectAttachment edges,
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly.
const (
	relationshipTypeInterconnectAttachmentUsesRouter       = "interconnect_attachment_uses_router"
	relationshipTypeInterconnectAttachmentUsesInterconnect = "interconnect_attachment_uses_interconnect"
	relationshipTypeInterconnectAttachmentUsesNetwork      = "interconnect_attachment_uses_network"
)

// interconnectAttachmentData is the bounded view of a CAI
// compute.googleapis.com/InterconnectAttachment resource.data blob. Every
// candidate/customer/cloud-router IP address field the Compute API exposes
// (candidateCloudRouterIpAddress, candidateCustomerRouterIpAddress,
// cloudRouterIpAddress, customerRouterIpAddress, and their IPv6 counterparts,
// plus candidateSubnets and ipsecInternalAddresses) is intentionally omitted
// from this struct: encoding/json silently ignores JSON object keys with no
// matching struct field, so these link-local addresses and address URLs are
// never decoded into Go memory at all, per the GCP collector contract Payload
// Boundaries. PartnerAsn is a string-encoded int64 per the compute API
// convention and is decoded via json.RawMessage/parseFlexibleInt64 so an
// absent value (most DEDICATED attachments) is distinguished from a
// legitimately present zero. L2Forwarding is present only for a
// `type: L2_DEDICATED` attachment, per the live Compute v1 discovery document
// (`l2Forwarding` "is required if the type is L2_DEDICATED"); the top-level
// InterconnectAttachment resource itself carries no `network` field at all —
// only the nested `l2Forwarding.network` names the attached VPC network.
type interconnectAttachmentData struct {
	Region                 string                          `json:"region"`
	Router                 string                          `json:"router"`
	Interconnect           string                          `json:"interconnect"`
	Type                   string                          `json:"type"`
	Bandwidth              string                          `json:"bandwidth"`
	EdgeAvailabilityDomain string                          `json:"edgeAvailabilityDomain"`
	State                  string                          `json:"state"`
	PartnerAsn             json.RawMessage                 `json:"partnerAsn"`
	CreationTimestamp      string                          `json:"creationTimestamp"`
	L2Forwarding           *interconnectAttachmentL2Config `json:"l2Forwarding"`
}

// interconnectAttachmentL2Config is the bounded view of an L2_DEDICATED
// attachment's `l2Forwarding` block. TunnelEndpointIpAddress and
// DefaultApplianceIpAddress are intentionally omitted from this struct:
// encoding/json silently ignores JSON object keys with no matching struct
// field, so neither IP address is ever decoded into Go memory, matching the
// same Payload Boundaries treatment as every other IP-address field on this
// resource. ApplianceMappings (per-VLAN-tag appliance IP routing) is likewise
// never decoded, since every entry resolves to an IP address.
type interconnectAttachmentL2Config struct {
	Network string `json:"network"`
}

func init() {
	RegisterAssetExtractor(assetTypeComputeInterconnectAttachment, extractInterconnectAttachment)
}

// extractInterconnectAttachment extracts bounded, redaction-safe typed depth
// for one compute InterconnectAttachment CAI asset. It returns the
// Terraform/drift/monitoring attribute set (region, attachment type,
// provisioned bandwidth, edge availability domain, state, partner ASN when
// present, and creation time); the resolved Cloud Router, Interconnect, and
// (for an L2_DEDICATED attachment only) the l2Forwarding VPC Network as
// correlation anchors; and the typed edges to those resources. No candidate,
// customer, or cloud-router IP address ever reaches the output, since
// interconnectAttachmentData declares no struct field for any of them.
func extractInterconnectAttachment(ctx ExtractContext) (AttributeExtraction, error) {
	var data interconnectAttachmentData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode interconnect attachment data: %w", err)
	}

	attrs := interconnectAttachmentAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if routerName := computeResourceFullNameFromSelfLink(data.Router, "routers", ctx.ProjectID); routerName != "" {
		anchors = append(anchors, routerName)
		rels = append(rels, interconnectAttachmentEdge(ctx, relationshipTypeInterconnectAttachmentUsesRouter, routerName, assetTypeComputeRouter))
	}
	if interconnectName := computeResourceFullNameFromSelfLink(data.Interconnect, "interconnects", ctx.ProjectID); interconnectName != "" {
		anchors = append(anchors, interconnectName)
		rels = append(rels, interconnectAttachmentEdge(ctx, relationshipTypeInterconnectAttachmentUsesInterconnect, interconnectName, assetTypeComputeInterconnect))
	}
	if data.L2Forwarding != nil {
		if networkName := computeFullResourceNameFromSelfLink(data.L2Forwarding.Network, ctx.ProjectID); networkName != "" {
			anchors = append(anchors, networkName)
			rels = append(rels, interconnectAttachmentEdge(ctx, relationshipTypeInterconnectAttachmentUsesNetwork, networkName, assetTypeComputeNetwork))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// interconnectAttachmentAttributes assembles the bounded attribute map. Empty
// or absent fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture (for example a false empty
// state). partnerAsn is decoded via parseFlexibleInt64 so an absent value
// (the common case for DEDICATED attachments, where the field is not
// available) is distinguished from a legitimately present zero.
func interconnectAttachmentAttributes(data interconnectAttachmentData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["type"] = v
	}
	if v := strings.TrimSpace(data.Bandwidth); v != "" {
		attrs["bandwidth"] = v
	}
	if v := strings.TrimSpace(data.EdgeAvailabilityDomain); v != "" {
		attrs["edge_availability_domain"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v, ok := parseFlexibleInt64(data.PartnerAsn); ok {
		attrs["partner_asn"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// interconnectAttachmentEdge builds a supported typed relationship
// observation rooted at the InterconnectAttachment.
func interconnectAttachmentEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
