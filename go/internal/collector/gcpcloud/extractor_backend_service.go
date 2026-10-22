// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_compute_lb.go (#6696 HK slice); uses below name catalog.X.

// backendServiceGroupSegments maps the compute resource-path segment found in
// a backend entry's `group` self-link to its CAI asset type, so a new backend
// group kind is a single table entry rather than a new branch.
var backendServiceGroupSegments = map[string]string{
	"instanceGroups":        catalog.AssetTypeComputeInstanceGroup,
	"networkEndpointGroups": catalog.AssetTypeComputeNetworkEndpointGroup,
}

// init registers extractBackendService for both distinct CAI asset types that
// carry BackendService.data: the global/search-API
// compute.googleapis.com/BackendService and the regional
// compute.googleapis.com/RegionBackendService the list/export/monitor/query
// path emits for a regional backend service. Both asset types share the same
// resource.data shape, so one extractor function handles both; this mirrors
// the ForwardingRule/GlobalForwardingRule and Address/GlobalAddress
// registrations.
func init() {
	RegisterAssetExtractor(catalog.AssetTypeComputeBackendService, extractBackendService)
	RegisterAssetExtractor(catalog.AssetTypeComputeRegionBackendService, extractBackendService)
}

// backendServiceData is the bounded view of a CAI
// compute.googleapis.com/BackendService resource.data blob. IAP
// (Identity-Aware Proxy) OAuth client id/secret and CDN cache-key/signed-URL
// key fields are intentionally NOT decoded here: per the GCP collector
// contract Payload Boundaries, no secret or key material is ever read.
// Backend entries decode only the `group` reference; balancing-mode,
// capacity, and utilization fields are data-plane tuning values, not typed
// depth, and are dropped by omission from backendServiceBackendData.
// EdgeSecurityPolicy is decoded separately from SecurityPolicy: Compute
// exposes them as two distinct resource-URL fields (regular Cloud Armor
// policy vs. edge Cloud Armor policy), and a backend service protected only
// by an edge security policy would otherwise emit no security-policy anchor
// or edge at all.
type backendServiceData struct {
	Region              string                      `json:"region"`
	Protocol            string                      `json:"protocol"`
	LoadBalancingScheme string                      `json:"loadBalancingScheme"`
	PortName            string                      `json:"portName"`
	TimeoutSec          *int64                      `json:"timeoutSec"`
	EnableCDN           *bool                       `json:"enableCDN"`
	SessionAffinity     string                      `json:"sessionAffinity"`
	SecurityPolicy      string                      `json:"securityPolicy"`
	EdgeSecurityPolicy  string                      `json:"edgeSecurityPolicy"`
	HealthChecks        []string                    `json:"healthChecks"`
	Backends            []backendServiceBackendData `json:"backends"`
	CreationTimestamp   string                      `json:"creationTimestamp"`
}

// backendServiceBackendData is the bounded view of one entry in a
// BackendService's `backends` array. Only the `group` self-link is decoded;
// balancingMode, capacityScaler, maxUtilization, and similar tuning fields
// are dropped by omission.
type backendServiceBackendData struct {
	Group string `json:"group"`
}

// extractBackendService extracts bounded, redaction-safe typed depth for one
// compute BackendService or RegionBackendService CAI asset (both asset types
// share this resource.data shape and register to this function; see the
// init() doc comment). It returns the Terraform/drift/monitoring attribute
// set (protocol, load-balancing scheme, port name, timeout, CDN and
// session-affinity posture, region omitted for a global backend service, and
// a backend-entry count), cross-source correlation anchors for the resolvable
// security policy, edge security policy, health checks, and backend groups,
// and the typed edges to those resources. IAP client secrets and CDN
// cache-key/signed-URL key material are never decoded, so no secret reaches a
// fact.
func extractBackendService(ctx ExtractContext) (AttributeExtraction, error) {
	var data backendServiceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode backend service data: %w", err)
	}

	attrs := backendServiceAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if policyName := computeResourceFullNameFromSelfLink(data.SecurityPolicy, "securityPolicies", ctx.ProjectID); policyName != "" {
		anchors = append(anchors, policyName)
		rels = append(rels, backendServiceEdge(ctx, catalog.RelationshipTypeBackendServiceUsesSecurityPolicy, policyName, catalog.AssetTypeComputeSecurityPolicy))
	}
	if edgePolicyName := computeResourceFullNameFromSelfLink(data.EdgeSecurityPolicy, "securityPolicies", ctx.ProjectID); edgePolicyName != "" {
		anchors = append(anchors, edgePolicyName)
		rels = append(rels, backendServiceEdge(ctx, catalog.RelationshipTypeBackendServiceUsesEdgeSecurityPolicy, edgePolicyName, catalog.AssetTypeComputeSecurityPolicy))
	}
	for _, hc := range data.HealthChecks {
		if hcName := computeResourceFullNameFromSelfLink(hc, "healthChecks", ctx.ProjectID); hcName != "" {
			anchors = append(anchors, hcName)
			rels = append(rels, backendServiceEdge(ctx, catalog.RelationshipTypeBackendServiceUsesHealthCheck, hcName, catalog.AssetTypeComputeHealthCheck))
		}
	}
	for _, backend := range data.Backends {
		groupType, groupName := backendServiceGroupEdge(backend.Group, ctx.ProjectID)
		if groupName == "" {
			continue
		}
		anchors = append(anchors, groupName)
		rels = append(rels, backendServiceEdge(ctx, catalog.RelationshipTypeBackendServiceHasBackend, groupName, groupType))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// backendServiceAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture, except EnableCDN which is a *bool
// and so distinguishes an explicit false (CDN disabled — a meaningful
// posture) from an absent field. A global backend service reports no region
// segment, so region is omitted rather than fabricated as "global".
func backendServiceAttributes(data backendServiceData) map[string]any {
	attrs := map[string]any{}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.Protocol); v != "" {
		attrs["protocol"] = v
	}
	if v := strings.TrimSpace(data.LoadBalancingScheme); v != "" {
		attrs["load_balancing_scheme"] = v
	}
	if v := strings.TrimSpace(data.PortName); v != "" {
		attrs["port_name"] = v
	}
	if data.TimeoutSec != nil {
		attrs["timeout_sec"] = *data.TimeoutSec
	}
	if data.EnableCDN != nil {
		attrs["enable_cdn"] = *data.EnableCDN
	}
	if v := strings.TrimSpace(data.SessionAffinity); v != "" {
		attrs["session_affinity"] = v
	}
	if n := len(data.Backends); n > 0 {
		attrs["backend_count"] = n
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// backendServiceGroupEdge resolves a backend entry's `group` reference (a
// full self-link or partial path) into its CAI asset type and full resource
// name via backendServiceGroupSegments. It returns a blank name for an empty,
// unrecognized, or unresolvable reference, so the caller emits no edge and no
// anchor for an ambiguous backend group.
func backendServiceGroupEdge(group, sourceProjectID string) (assetType, fullName string) {
	trimmed := strings.TrimSpace(group)
	if trimmed == "" {
		return "", ""
	}
	for segment, mapping := range backendServiceGroupSegments {
		if name := computeResourceFullNameFromSelfLink(trimmed, segment, sourceProjectID); name != "" {
			return mapping, name
		}
	}
	return "", ""
}

// backendServiceEdge builds a supported typed relationship observation rooted
// at the backend service.
func backendServiceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
