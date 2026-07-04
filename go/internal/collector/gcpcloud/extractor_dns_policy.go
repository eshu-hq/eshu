// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
)

// dnsPolicyAssetType is the Cloud Asset Inventory asset type for a Cloud DNS
// Policy. It is distinct from dnsManagedZoneAssetType: a Policy binds
// inbound-forwarding, logging, and alternative-name-server behavior to a set
// of VPC networks, while a ManagedZone is a DNS namespace with its own
// visibility and peering configuration.
const dnsPolicyAssetType = "dns.googleapis.com/Policy"

// relationshipTypeDNSPolicyAppliesToNetwork marks a DNS Policy's bind to a VPC
// network it governs (networks[].networkUrl). It is a stable provider
// relationship string carried on gcp_cloud_relationship facts; the reducer
// materializes an edge only when both endpoints resolve exactly.
const relationshipTypeDNSPolicyAppliesToNetwork = "dns_policy_applies_to_network"

// dnsPolicyTargetNameServer is the bounded, redaction-safe view of one
// alternativeNameServerConfig.targetNameServers[] entry. Per the live Cloud
// DNS v1 discovery document, the API's TargetNameServer shape carries
// ipv4Address, ipv6Address, and forwardingPath; only forwardingPath is a
// bounded control-plane enum ("default"/"private") safe to decode. Neither
// ipv4Address nor ipv6Address has a Go struct field here — not even as
// json.RawMessage — so the raw address value never exists as a decoded Go
// value, closing the gap where a future debug/error path might serialize the
// decode target itself rather than the returned AttributeExtraction.
type dnsPolicyTargetNameServer struct {
	ForwardingPath string `json:"forwardingPath"`
}

// dnsPolicyData is the bounded view of a CAI dns.googleapis.com/Policy
// resource.data blob. EnableInboundForwarding and EnableLogging are *bool:
// the Cloud DNS v1 discovery document defines both as plain proto3 boolean
// fields ("Defaults to no logging if not set"), so a real CAI page omits the
// field entirely at its proto3 default (false) — identical to the Backend
// Service extractor's EnableCDN. A pointer distinguishes an explicit false
// (forwarding/logging disabled — a meaningful posture operators alert on)
// from an absent field, so a partial CAI page never fabricates a posture.
// description is intentionally NOT decoded into an attribute: it is
// free-form operator text, not a bounded control-plane field usable for
// Terraform import/drift, edges, correlation, or monitoring, per the same
// treatment the DNS Managed Zone extractor gives its own dnsName.
// alternativeNameServerConfig.targetNameServers[] decodes only the bounded
// dnsPolicyTargetNameServer shape (forwardingPath); the address fields are
// read only to count entries and are never decoded into a Go field the
// extractor can accidentally surface.
type dnsPolicyData struct {
	EnableInboundForwarding *bool `json:"enableInboundForwarding"`
	EnableLogging           *bool `json:"enableLogging"`
	Networks                []struct {
		NetworkURL string `json:"networkUrl"`
	} `json:"networks"`
	AlternativeNameServerConfig *struct {
		TargetNameServers []dnsPolicyTargetNameServer `json:"targetNameServers"`
	} `json:"alternativeNameServerConfig"`
}

func init() {
	RegisterAssetExtractor(dnsPolicyAssetType, extractDNSPolicy)
}

// extractDNSPolicy extracts bounded, redaction-safe typed depth for one Cloud
// DNS Policy CAI asset. It returns the Terraform/drift/monitoring attribute
// set (inbound-forwarding posture, logging posture — both explicit tri-state
// so a real false is kept distinct from an absent field — bound-network
// count, and alternative-name-server count); the bound networks as
// correlation anchors; and a typed dns_policy_applies_to_network edge per
// resolved network. Bound networks are deduplicated by resolved full
// resource name before network_count, anchors, and edges are derived, so a
// duplicate networks[] entry (or two networkUrl values resolving to the same
// full resource name) can never overcount or emit a duplicate edge. The
// policy's own description and alternative-name-server addresses are never
// decoded into an attribute or anchor.
func extractDNSPolicy(ctx ExtractContext) (AttributeExtraction, error) {
	var data dnsPolicyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dns policy data: %w", err)
	}

	attrs := map[string]any{}
	var anchors []string
	var rels []RelationshipObservation

	if data.EnableInboundForwarding != nil {
		attrs["enable_inbound_forwarding"] = *data.EnableInboundForwarding
	}
	if data.EnableLogging != nil {
		attrs["enable_logging"] = *data.EnableLogging
	}

	var resolvedNames []string
	for _, network := range data.Networks {
		name := computeFullResourceNameFromSelfLink(network.NetworkURL, ctx.ProjectID)
		if name == "" {
			continue
		}
		resolvedNames = append(resolvedNames, name)
	}
	// Dedup by resolved full resource name before counting or emitting
	// relationships: duplicate networks[] entries (or distinct networkUrl
	// values that resolve to the same full resource name) must not inflate
	// network_count nor produce duplicate edges, matching the dedup already
	// applied to CorrelationAnchors below.
	dedupedNames := dedupeNonEmpty(resolvedNames)
	for _, name := range dedupedNames {
		anchors = append(anchors, name)
		rels = append(rels, dnsPolicyEdge(ctx, name))
	}
	if len(dedupedNames) > 0 {
		attrs["network_count"] = len(dedupedNames)
	}

	if data.AlternativeNameServerConfig != nil {
		if n := len(data.AlternativeNameServerConfig.TargetNameServers); n > 0 {
			attrs["alternative_name_server_count"] = n
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dnsPolicyEdge builds a supported typed relationship observation rooted at
// the DNS Policy, targeting a VPC Network.
func dnsPolicyEdge(ctx ExtractContext, targetName string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipTypeDNSPolicyAppliesToNetwork,
		TargetFullResourceName: targetName,
		TargetAssetType:        assetTypeComputeNetwork,
		SupportState:           RelationshipSupportSupported,
	}
}
