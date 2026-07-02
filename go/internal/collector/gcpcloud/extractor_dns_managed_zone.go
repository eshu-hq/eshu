// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// dnsManagedZoneAssetType is the Cloud Asset Inventory asset type for a Cloud
// DNS Managed Zone. It is distinct from assetTypeDNSResourceRecordSet, which
// covers the zone's own record sets and flows through the separate
// gcp_dns_record fact family (parseDNSRecords), not this typed-depth extractor.
const dnsManagedZoneAssetType = "dns.googleapis.com/ManagedZone"

// Bounded provider relationship types for the Managed Zone's network edges.
// They are stable provider relationship strings carried on
// gcp_cloud_relationship facts; the reducer materializes an edge only when both
// endpoints resolve exactly.
const (
	// relationshipTypeManagedZoneVisibleFromNetwork marks a private zone's bind
	// to a VPC network that can resolve it (privateVisibilityConfig.networks).
	relationshipTypeManagedZoneVisibleFromNetwork = "dns_managed_zone_visible_from_network"
	// relationshipTypeManagedZonePeersWithNetwork marks a DNS peering zone's
	// forwarding bind to the peer VPC network (peeringConfig.targetNetwork).
	relationshipTypeManagedZonePeersWithNetwork = "dns_managed_zone_peers_with_network"
)

// dnsManagedZoneData is the bounded view of a CAI dns.googleapis.com/ManagedZone
// resource.data blob. dnsName is intentionally NOT decoded into an attribute:
// the typed-depth extractor seam carries no redaction key (see
// AttributeExtraction), and a zone's DNS name is DNS-name text exactly like the
// record name/target values the sibling gcp_dns_record fact family fingerprints
// (dns_record.go), so it must be fingerprinted or omitted, never persisted raw.
// Labels are handled by the existing base-observation label path, not this
// extractor. Forwarding target IPs (targetNameServers[].ipv4Address /
// ipv6Address) are read only to count entries; the address values themselves
// are never decoded into a Go field the extractor can accidentally surface.
type dnsManagedZoneData struct {
	Visibility              string `json:"visibility"`
	CreationTime            string `json:"creationTime"`
	PrivateVisibilityConfig *struct {
		Networks []struct {
			NetworkURL string `json:"networkUrl"`
		} `json:"networks"`
	} `json:"privateVisibilityConfig"`
	ForwardingConfig *struct {
		TargetNameServers []json.RawMessage `json:"targetNameServers"`
	} `json:"forwardingConfig"`
	PeeringConfig *struct {
		TargetNetwork *struct {
			NetworkURL string `json:"networkUrl"`
		} `json:"targetNetwork"`
	} `json:"peeringConfig"`
	DNSSECConfig *struct {
		State string `json:"state"`
	} `json:"dnssecConfig"`
}

func init() {
	RegisterAssetExtractor(dnsManagedZoneAssetType, extractDNSManagedZone)
}

// extractDNSManagedZone extracts bounded, redaction-safe typed depth for one
// Cloud DNS Managed Zone CAI asset. It returns the Terraform/drift/monitoring
// attribute set (visibility, DNSSEC state, private-network count, forwarding
// posture and target count, peering posture, and creation time); the bound
// private-visibility networks and the DNS-peering target network as
// correlation anchors; and the typed visible-from-network and
// peers-with-network edges. The zone's own dnsName, forwarding target IPs, and
// forwarding target hostnames are never decoded into an attribute or anchor.
func extractDNSManagedZone(ctx ExtractContext) (AttributeExtraction, error) {
	var data dnsManagedZoneData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dns managed zone data: %w", err)
	}

	attrs := map[string]any{}
	var anchors []string
	var rels []RelationshipObservation

	if v := strings.TrimSpace(data.Visibility); v != "" {
		attrs["visibility"] = v
	}
	if data.DNSSECConfig != nil {
		if state := strings.TrimSpace(data.DNSSECConfig.State); state != "" {
			attrs["dnssec_state"] = state
		}
	}
	if v, ok := normalizeRFC3339(data.CreationTime); ok {
		attrs["creation_time"] = v
	}

	if data.PrivateVisibilityConfig != nil {
		var networkCount int
		for _, network := range data.PrivateVisibilityConfig.Networks {
			name := computeFullResourceNameFromSelfLink(network.NetworkURL, ctx.ProjectID)
			if name == "" {
				continue
			}
			networkCount++
			anchors = append(anchors, name)
			rels = append(rels, dnsManagedZoneEdge(ctx, relationshipTypeManagedZoneVisibleFromNetwork, name))
		}
		if networkCount > 0 {
			attrs["private_network_count"] = networkCount
		}
	}

	if data.ForwardingConfig != nil {
		if n := len(data.ForwardingConfig.TargetNameServers); n > 0 {
			attrs["forwarding_enabled"] = true
			attrs["forwarding_target_count"] = n
		}
	}

	if data.PeeringConfig != nil && data.PeeringConfig.TargetNetwork != nil {
		attrs["is_peering_zone"] = true
		name := computeFullResourceNameFromSelfLink(data.PeeringConfig.TargetNetwork.NetworkURL, ctx.ProjectID)
		if name != "" {
			anchors = append(anchors, name)
			rels = append(rels, dnsManagedZoneEdge(ctx, relationshipTypeManagedZonePeersWithNetwork, name))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dnsManagedZoneEdge builds a supported typed relationship observation rooted
// at the Managed Zone, targeting a VPC Network.
func dnsManagedZoneEdge(ctx ExtractContext, relationshipType, targetName string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        assetTypeComputeNetwork,
		SupportState:           RelationshipSupportSupported,
	}
}
