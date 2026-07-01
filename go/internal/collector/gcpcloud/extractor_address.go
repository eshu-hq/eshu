// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute Static Address typed-depth extractor and
// the relationship endpoints it derives. assetTypeComputeNetwork,
// assetTypeComputeSubnetwork, and assetTypeComputeInstance are declared by the
// sibling compute extractors in this package and reused here.
const (
	assetTypeComputeAddress        = "compute.googleapis.com/Address"
	assetTypeComputeGlobalAddress  = "compute.googleapis.com/GlobalAddress"
	assetTypeComputeForwardingRule = "compute.googleapis.com/ForwardingRule"
)

// Bounded provider relationship types for the Static Address edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when both
// endpoints resolve exactly.
const (
	relationshipTypeAddressInNetwork            = "address_in_network"
	relationshipTypeAddressInSubnetwork         = "address_in_subnetwork"
	relationshipTypeAddressUsedByForwardingRule = "address_used_by_forwarding_rule"
	relationshipTypeAddressUsedByInstance       = "address_used_by_instance"
)

// addressData is the bounded view of a CAI compute.googleapis.com/Address
// resource.data blob. The raw `address` IP value is intentionally NOT a decoded
// field — the external-vs-internal exposure posture comes from addressType, so no
// public or private IP address is ever read, per the GCP collector contract
// Payload Boundaries. Only redaction-safe control-plane metadata and resource
// references are decoded.
type addressData struct {
	AddressType       string   `json:"addressType"`
	Purpose           string   `json:"purpose"`
	Status            string   `json:"status"`
	IPVersion         string   `json:"ipVersion"`
	Region            string   `json:"region"`
	Network           string   `json:"network"`
	Subnetwork        string   `json:"subnetwork"`
	Users             []string `json:"users"`
	CreationTimestamp string   `json:"creationTimestamp"`
}

func init() {
	// Global static IPs are reported by Cloud Asset Inventory as the distinct
	// GlobalAddress asset type; they carry the same resource.data shape (minus
	// region/subnetwork), so the same extractor handles both.
	RegisterAssetExtractor(assetTypeComputeAddress, extractAddress)
	RegisterAssetExtractor(assetTypeComputeGlobalAddress, extractAddress)
}

// extractAddress extracts bounded, redaction-safe typed depth for one compute
// Static Address CAI asset. It returns the Terraform/drift/monitoring attribute
// set (region, address type and external-exposure flag, purpose, status, IP
// version, creation time, and user count); the enclosing network and subnetwork
// plus each resolvable using resource (forwarding rule or instance) as
// correlation anchors; and the typed network, subnetwork, and used-by edges. The
// reserved IP address value itself is never decoded, so no address reaches a
// fact.
func extractAddress(ctx ExtractContext) (AttributeExtraction, error) {
	var data addressData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode address data: %w", err)
	}

	attrs := addressAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, addressEdge(ctx, relationshipTypeAddressInNetwork, networkName, assetTypeComputeNetwork))
	}
	if subnetName := computeFullResourceNameFromSelfLink(data.Subnetwork, ctx.ProjectID); subnetName != "" {
		anchors = append(anchors, subnetName)
		rels = append(rels, addressEdge(ctx, relationshipTypeAddressInSubnetwork, subnetName, assetTypeComputeSubnetwork))
	}
	// Deduplicate users by resolved resource name so a duplicated users[] entry
	// does not over-count user_count relative to the distinct used-by edges.
	seenUsers := map[string]bool{}
	for _, user := range data.Users {
		if frName := computeResourceFullName(user, "forwardingRules"); frName != "" {
			if seenUsers[frName] {
				continue
			}
			seenUsers[frName] = true
			anchors = append(anchors, frName)
			rels = append(rels, addressEdge(ctx, relationshipTypeAddressUsedByForwardingRule, frName, assetTypeComputeForwardingRule))
			continue
		}
		if instanceName := computeResourceFullName(user, "instances"); instanceName != "" {
			if seenUsers[instanceName] {
				continue
			}
			seenUsers[instanceName] = true
			anchors = append(anchors, instanceName)
			rels = append(rels, addressEdge(ctx, relationshipTypeAddressUsedByInstance, instanceName, assetTypeComputeInstance))
		}
	}
	if n := len(seenUsers); n > 0 {
		attrs["user_count"] = n
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// addressAttributes assembles the bounded attribute map. Empty or absent fields
// are omitted rather than written as zero values so a partial CAI page does not
// fabricate a posture. The external-exposure flag is derived from addressType
// (EXTERNAL), never from the address value.
func addressAttributes(data addressData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.AddressType); v != "" {
		attrs["address_type"] = v
		attrs["is_external"] = strings.EqualFold(v, "EXTERNAL")
	}
	if v := strings.TrimSpace(data.Purpose); v != "" {
		attrs["purpose"] = v
	}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if v := strings.TrimSpace(data.IPVersion); v != "" {
		attrs["ip_version"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// addressEdge builds a supported typed relationship observation rooted at the
// address.
func addressEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
