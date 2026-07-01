// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeComputeFirewall is the CAI asset type for a VPC firewall rule. The
// target network asset type (assetTypeComputeNetwork) and the compute
// name-derivation helpers are shared with the sibling compute extractors.
const assetTypeComputeFirewall = "compute.googleapis.com/Firewall"

// relationshipTypeFirewallAppliesToNetwork is the bounded provider relationship
// type for the firewall -> enclosing VPC network edge carried on a
// gcp_cloud_relationship fact. The reducer materializes it only when both
// endpoints resolve exactly. The firewall's target service accounts are surfaced
// as fingerprinted correlation anchors rather than edges: a service-account
// email is not an exactly resolvable CAI endpoint, and the "applies to instances
// running as SA" join is owned by the secrets/IAM layer keying on the digest.
const relationshipTypeFirewallAppliesToNetwork = "firewall_applies_to_network"

// firewallData is the bounded view of a CAI compute.googleapis.com/Firewall
// resource.data blob. Address-bearing fields (sourceRanges, destinationRanges)
// are decoded only to derive a non-address exposure signal and a count; their raw
// CIDR values never leave this extractor, per the GCP collector contract Payload
// Boundaries (no public or private IP addresses persisted). Disabled is a pointer
// so a present false (an enabled rule) is distinguishable from an absent field.
type firewallData struct {
	Network               string          `json:"network"`
	Direction             string          `json:"direction"`
	Priority              json.RawMessage `json:"priority"`
	Disabled              *bool           `json:"disabled"`
	SourceRanges          []string        `json:"sourceRanges"`
	DestinationRanges     []string        `json:"destinationRanges"`
	TargetTags            []string        `json:"targetTags"`
	TargetServiceAccounts []string        `json:"targetServiceAccounts"`
	Allowed               []firewallRule  `json:"allowed"`
	Denied                []firewallRule  `json:"denied"`
	LogConfig             *struct {
		Enable *bool `json:"enable"`
	} `json:"logConfig"`
}

// firewallRule is one allow/deny protocol entry. Protocols (tcp/udp/icmp/all) and
// ports are control-plane values, not addresses, and are safe to keep for
// Terraform drift.
type firewallRule struct {
	IPProtocol string   `json:"IPProtocol"`
	Ports      []string `json:"ports"`
}

func init() {
	RegisterAssetExtractor(assetTypeComputeFirewall, extractFirewall)
}

// extractFirewall extracts bounded, redaction-safe typed depth for one VPC
// Firewall CAI asset. It returns the Terraform/drift/monitoring attribute set
// (direction, priority, disabled/log posture, allow/deny protocols and ports,
// source/destination range counts, a public-exposure signal, target tags, and
// the target service-account count); the enclosing network as a correlation
// anchor plus the fingerprinted target service-account emails; and the typed
// firewall_applies_to_network edge. No IP range value or raw service-account
// email reaches the output.
func extractFirewall(ctx ExtractContext) (AttributeExtraction, error) {
	var data firewallData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode firewall data: %w", err)
	}

	attrs := firewallAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, firewallEdge(ctx, relationshipTypeFirewallAppliesToNetwork, networkName, assetTypeComputeNetwork))
	}
	for _, email := range data.TargetServiceAccounts {
		if digest := secretsiam.GCPServiceAccountEmailDigest(email); digest != "" {
			anchors = append(anchors, digest)
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// firewallAttributes assembles the bounded attribute map. Empty or absent fields
// are omitted rather than written as zero values so a partial CAI page does not
// fabricate a posture. Source/destination ranges are reduced to counts and a
// public-exposure boolean; the CIDR values themselves are never persisted.
func firewallAttributes(data firewallData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Direction); v != "" {
		attrs["direction"] = v
	}
	if v, ok := parseFlexibleInt64(data.Priority); ok {
		attrs["priority"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}
	if data.LogConfig != nil && data.LogConfig.Enable != nil {
		attrs["log_enabled"] = *data.LogConfig.Enable
	}
	if protocols := ruleProtocols(data.Allowed); len(protocols) > 0 {
		attrs["allowed_protocols"] = protocols
	}
	if ports := rulePorts(data.Allowed); len(ports) > 0 {
		attrs["allowed_ports"] = ports
	}
	if protocols := ruleProtocols(data.Denied); len(protocols) > 0 {
		attrs["denied_protocols"] = protocols
	}
	if ports := rulePorts(data.Denied); len(ports) > 0 {
		attrs["denied_ports"] = ports
	}
	if n := len(data.SourceRanges); n > 0 {
		attrs["source_range_count"] = n
		attrs["opens_to_public"] = rangesOpenToPublic(data.SourceRanges)
	}
	if n := len(data.DestinationRanges); n > 0 {
		attrs["destination_range_count"] = n
	}
	if tags := dedupeNonEmpty(data.TargetTags); len(tags) > 0 {
		attrs["target_tags"] = tags
	}
	if n := len(data.TargetServiceAccounts); n > 0 {
		attrs["target_service_account_count"] = n
	}
	return attrs
}

// ruleProtocols returns the deduplicated IP protocols across a set of allow/deny
// rules (for example tcp, udp, icmp, all).
func ruleProtocols(rules []firewallRule) []string {
	protocols := make([]string, 0, len(rules))
	for _, r := range rules {
		protocols = append(protocols, r.IPProtocol)
	}
	return dedupeNonEmpty(protocols)
}

// rulePorts returns the deduplicated ports/port-ranges across a set of allow/deny
// rules. Ports are control-plane values (not addresses), kept for Terraform drift.
func rulePorts(rules []firewallRule) []string {
	var ports []string
	for _, r := range rules {
		ports = append(ports, r.Ports...)
	}
	return dedupeNonEmpty(ports)
}

// rangesOpenToPublic reports whether any range is the IPv4 or IPv6 "any"
// address (0.0.0.0/0 or ::/0), the public-exposure signal. It reads the ranges
// only to compute this boolean; the CIDR values are never persisted.
func rangesOpenToPublic(ranges []string) bool {
	for _, r := range ranges {
		switch strings.TrimSpace(r) {
		case "0.0.0.0/0", "::/0":
			return true
		}
	}
	return false
}

// firewallEdge builds a supported typed relationship observation rooted at the
// firewall.
func firewallEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
