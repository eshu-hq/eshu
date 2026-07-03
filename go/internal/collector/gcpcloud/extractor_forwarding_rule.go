// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute ForwardingRule typed-depth extractor and
// the relationship endpoints it derives. assetTypeComputeForwardingRule and
// assetTypeComputeGlobalForwardingRule are declared in extractor_address.go
// (assetTypeComputeForwardingRule is reused there by the Address extractor for
// its used-by-forwarding-rule edge) and reused here.
// assetTypeComputeNetwork and assetTypeComputeSubnetwork are declared by the
// sibling Network/Subnetwork extractors and reused here.
const (
	assetTypeComputeBackendService    = "compute.googleapis.com/BackendService"
	assetTypeComputeTargetPool        = "compute.googleapis.com/TargetPool"
	assetTypeComputeTargetHTTPProxy   = "compute.googleapis.com/TargetHttpProxy"
	assetTypeComputeTargetHTTPSProxy  = "compute.googleapis.com/TargetHttpsProxy"
	assetTypeComputeTargetTCPProxy    = "compute.googleapis.com/TargetTcpProxy"
	assetTypeComputeTargetSSLProxy    = "compute.googleapis.com/TargetSslProxy"
	assetTypeComputeTargetGRPCProxy   = "compute.googleapis.com/TargetGrpcProxy"
	assetTypeComputeTargetInstance    = "compute.googleapis.com/TargetInstance"
	assetTypeComputeTargetVPNGateway  = "compute.googleapis.com/TargetVpnGateway"
	assetTypeComputeServiceAttachment = "compute.googleapis.com/ServiceAttachment"
)

// Bounded provider relationship types for ForwardingRule edges. They are
// stable, bounded strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
// forwardingRuleTargetSegments maps the compute resource-path segment found in
// a ForwardingRule's `target` reference to its CAI asset type and bounded
// relationship type, so a new proxy kind is a single table entry rather than a
// new branch.
const (
	relationshipTypeForwardingRuleTargetsBackendService    = "forwarding_rule_targets_backend_service"
	relationshipTypeForwardingRuleTargetsTargetPool        = "forwarding_rule_targets_target_pool"
	relationshipTypeForwardingRuleTargetsTargetProxy       = "forwarding_rule_targets_target_proxy"
	relationshipTypeForwardingRuleTargetsTargetInstance    = "forwarding_rule_targets_target_instance"
	relationshipTypeForwardingRuleTargetsTargetVPNGateway  = "forwarding_rule_targets_target_vpn_gateway"
	relationshipTypeForwardingRuleTargetsServiceAttachment = "forwarding_rule_targets_service_attachment"
	relationshipTypeForwardingRuleInNetwork                = "forwarding_rule_in_network"
	relationshipTypeForwardingRuleInSubnetwork             = "forwarding_rule_in_subnetwork"
)

// forwardingRuleTargetSegments maps a compute resource-path segment to its CAI
// asset type and bounded relationship type for the ForwardingRule -> target
// edge. All proxy kinds share relationshipTypeForwardingRuleTargetsTargetProxy
// so an operator/query filters by target_asset_type for the proxy protocol
// rather than by a proliferating set of relationship-type strings.
var forwardingRuleTargetSegments = map[string]struct {
	assetType    string
	relationship string
}{
	"targetPools":        {assetTypeComputeTargetPool, relationshipTypeForwardingRuleTargetsTargetPool},
	"targetHttpProxies":  {assetTypeComputeTargetHTTPProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
	"targetHttpsProxies": {assetTypeComputeTargetHTTPSProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
	"targetTcpProxies":   {assetTypeComputeTargetTCPProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
	"targetSslProxies":   {assetTypeComputeTargetSSLProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
	"targetGrpcProxies":  {assetTypeComputeTargetGRPCProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
	"targetInstances":    {assetTypeComputeTargetInstance, relationshipTypeForwardingRuleTargetsTargetInstance},
	"targetVpnGateways":  {assetTypeComputeTargetVPNGateway, relationshipTypeForwardingRuleTargetsTargetVPNGateway},
	"serviceAttachments": {assetTypeComputeServiceAttachment, relationshipTypeForwardingRuleTargetsServiceAttachment},
}

// init registers extractForwardingRule for both distinct CAI asset types that
// carry ForwardingRule.target data: regional compute.googleapis.com/ForwardingRule
// and global compute.googleapis.com/GlobalForwardingRule (the load-balancer
// frontend asset type CAI emits for global forwarding rules). Both asset
// types share the same resource.data shape, so one extractor function
// handles both; this mirrors the Address/GlobalAddress registration in
// extractor_address.go.
func init() {
	RegisterAssetExtractor(assetTypeComputeForwardingRule, extractForwardingRule)
	RegisterAssetExtractor(assetTypeComputeGlobalForwardingRule, extractForwardingRule)
}

// forwardingRuleData is the bounded view of a CAI
// compute.googleapis.com/ForwardingRule resource.data blob. IPAddress is
// intentionally NOT a decoded field: per the GCP collector contract Payload
// Boundaries, no public or private IP address is ever read, mirroring the
// Static Address extractor's treatment of its own `address` field. Only
// redaction-safe control-plane metadata and resource references are decoded.
type forwardingRuleData struct {
	Region               string   `json:"region"`
	LoadBalancingScheme  string   `json:"loadBalancingScheme"`
	IPProtocol           string   `json:"IPProtocol"`
	PortRange            string   `json:"portRange"`
	Ports                []string `json:"ports"`
	Target               string   `json:"target"`
	BackendService       string   `json:"backendService"`
	Network              string   `json:"network"`
	Subnetwork           string   `json:"subnetwork"`
	IPVersion            string   `json:"ipVersion"`
	AllPorts             *bool    `json:"allPorts"`
	AllowGlobalAccess    *bool    `json:"allowGlobalAccess"`
	IsMirroringCollector *bool    `json:"isMirroringCollector"`
	NetworkTier          string   `json:"networkTier"`
	CreationTimestamp    string   `json:"creationTimestamp"`
}

// extractForwardingRule extracts bounded, redaction-safe typed depth for one
// compute ForwardingRule CAI asset (the GCP load-balancer forwarding-rule
// resource). It returns the Terraform/drift/monitoring attribute set,
// cross-source correlation anchors for the resolvable target/backend
// service/network/subnetwork, and the typed edges to those resources. The
// reserved IPAddress value is never decoded, so no address reaches a fact; the
// external-vs-internal exposure posture comes from loadBalancingScheme.
func extractForwardingRule(ctx ExtractContext) (AttributeExtraction, error) {
	var data forwardingRuleData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode forwarding rule data: %w", err)
	}

	attrs := forwardingRuleAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if backendName := computeResourceFullName(data.BackendService, "backendServices"); backendName != "" {
		anchors = append(anchors, backendName)
		rels = append(rels, forwardingRuleEdge(ctx, relationshipTypeForwardingRuleTargetsBackendService, backendName, assetTypeComputeBackendService))
	}
	if targetType, relType, targetName := forwardingRuleTargetEdge(data.Target, ctx.ProjectID); targetName != "" {
		anchors = append(anchors, targetName)
		rels = append(rels, forwardingRuleEdge(ctx, relType, targetName, targetType))
	}
	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, forwardingRuleEdge(ctx, relationshipTypeForwardingRuleInNetwork, networkName, assetTypeComputeNetwork))
	}
	if subnetName := computeFullResourceNameFromSelfLink(data.Subnetwork, ctx.ProjectID); subnetName != "" {
		anchors = append(anchors, subnetName)
		rels = append(rels, forwardingRuleEdge(ctx, relationshipTypeForwardingRuleInSubnetwork, subnetName, assetTypeComputeSubnetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// forwardingRuleAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture. is_external is derived from
// loadBalancingScheme (EXTERNAL/EXTERNAL_MANAGED prefix), never from the
// IP address. A global forwarding rule reports no region segment, so region is
// omitted rather than fabricated as "global".
func forwardingRuleAttributes(data forwardingRuleData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.LoadBalancingScheme); v != "" {
		attrs["load_balancing_scheme"] = v
		attrs["is_external"] = strings.HasPrefix(strings.ToUpper(v), "EXTERNAL")
	}
	if v := strings.TrimSpace(data.IPProtocol); v != "" {
		attrs["ip_protocol"] = v
	}
	if v := strings.TrimSpace(data.PortRange); v != "" {
		attrs["port_range"] = v
	}
	if ports := dedupeNonEmpty(data.Ports); len(ports) > 0 {
		attrs["ports"] = ports
	}
	if v := strings.TrimSpace(data.IPVersion); v != "" {
		attrs["ip_version"] = v
	}
	if data.AllPorts != nil {
		attrs["all_ports"] = *data.AllPorts
	}
	if data.AllowGlobalAccess != nil {
		attrs["allow_global_access"] = *data.AllowGlobalAccess
	}
	if data.IsMirroringCollector != nil {
		attrs["is_mirroring_collector"] = *data.IsMirroringCollector
	}
	if v := strings.TrimSpace(data.NetworkTier); v != "" {
		attrs["network_tier"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// forwardingRuleTargetEdge resolves a ForwardingRule's `target` reference
// (a full self-link or partial path) into its CAI asset type, bounded
// relationship type, and full resource name. It recognizes every compute
// resource segment ForwardingRule.target may name (target pool, one of the
// proxy kinds, a target instance, a Classic VPN target gateway, or a PSC
// service attachment) via forwardingRuleTargetSegments. It returns a blank
// name for an empty, unrecognized, or unresolvable reference, so the caller
// emits no edge and no anchor for an ambiguous target.
func forwardingRuleTargetEdge(target, sourceProjectID string) (assetType, relationshipType, fullName string) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", "", ""
	}
	for segment, mapping := range forwardingRuleTargetSegments {
		if name := computeResourceFullNameFromSelfLink(trimmed, segment, sourceProjectID); name != "" {
			return mapping.assetType, mapping.relationship, name
		}
	}
	return "", "", ""
}

// computeResourceFullNameFromSelfLink resolves a compute resource reference
// into its CAI full resource name only when the path names the given resource
// segment (for example "targetPools" or "targetHttpsProxies"). It delegates to
// computeFullResourceNameFromSelfLink's self-link/partial-path normalization
// and then verifies the resolved path carries the expected segment, so a
// reference to a different resource kind never produces a false edge.
func computeResourceFullNameFromSelfLink(ref, segment, sourceProjectID string) string {
	name := computeFullResourceNameFromSelfLink(ref, sourceProjectID)
	if name == "" {
		return ""
	}
	if !strings.Contains(name, "/"+segment+"/") {
		return ""
	}
	return name
}

// forwardingRuleEdge builds a supported typed relationship observation rooted
// at the forwarding rule.
func forwardingRuleEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
