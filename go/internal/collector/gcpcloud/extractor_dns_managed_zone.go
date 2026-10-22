// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
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
	RegisterAssetExtractor(catalog.DNSManagedZoneAssetType, extractDNSManagedZone)
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
			rels = append(rels, dnsManagedZoneEdge(ctx, catalog.RelationshipTypeManagedZoneVisibleFromNetwork, name))
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
			rels = append(rels, dnsManagedZoneEdge(ctx, catalog.RelationshipTypeManagedZonePeersWithNetwork, name))
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
