// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constant for the compute BackendBucket relationship endpoint the
// URL Map extractor derives. assetTypeComputeBackendService is declared in
// extractor_forwarding_rule.go and reused here; assetTypeComputeUrlMap is
// declared here since URL Map is this extractor's own asset type.
const (
	assetTypeComputeUrlMap        = "compute.googleapis.com/UrlMap"
	assetTypeComputeBackendBucket = "compute.googleapis.com/BackendBucket"
)

// Bounded relationship types for URL Map edges. They are stable, bounded
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeURLMapDefaultService     = "url_map_default_service"
	relationshipTypeURLMapPathMatcherService = "url_map_path_matcher_default_service"
	relationshipTypeURLMapPathRuleService    = "url_map_path_rule_service"
)

func init() {
	RegisterAssetExtractor(assetTypeComputeUrlMap, extractURLMap)
}

// urlMapPathRuleData is the bounded view of one pathMatchers[].pathRules[]
// entry. Only the backend `service` reference is decoded; `paths` (URL path
// patterns) are routing logic the collector contract's Payload Boundaries
// bar from a fact, so only a bounded rule count is kept at the caller.
type urlMapPathRuleData struct {
	Service string `json:"service"`
}

// urlMapPathMatcherData is the bounded view of one pathMatchers[] entry.
// Only name, defaultService, and the pathRules service references are
// decoded; routeRules carry header/query-based routing logic and are never
// decoded beyond a bounded count.
type urlMapPathMatcherData struct {
	Name           string               `json:"name"`
	DefaultService string               `json:"defaultService"`
	PathRules      []urlMapPathRuleData `json:"pathRules"`
	RouteRules     []json.RawMessage    `json:"routeRules"`
}

// urlMapHostRuleData is the bounded view of one hostRules[] entry. Only the
// pathMatcher name reference is decoded; `hosts` are raw host patterns the
// collector contract's Payload Boundaries bar from a fact, so only a bounded
// host-rule count is kept at the caller.
type urlMapHostRuleData struct {
	PathMatcher string `json:"pathMatcher"`
}

// urlMapData is the bounded view of a CAI compute.googleapis.com/UrlMap
// resource.data blob. hostRules[].hosts and pathMatchers[].pathRules[].paths
// are intentionally NOT decoded fields: per the GCP collector contract
// Payload Boundaries, raw host/path routing patterns are never persisted —
// only bounded counts and the resolvable backend-service references reach a
// fact.
type urlMapData struct {
	DefaultService    string                  `json:"defaultService"`
	HostRules         []urlMapHostRuleData    `json:"hostRules"`
	PathMatchers      []urlMapPathMatcherData `json:"pathMatchers"`
	CreationTimestamp string                  `json:"creationTimestamp"`
}

// extractURLMap extracts bounded, redaction-safe typed depth for one compute
// UrlMap CAI asset (the GCP HTTP(S) load-balancer URL map resource). It
// returns the Terraform/drift/monitoring attribute set (bounded host-rule and
// path-matcher counts, creation time), cross-source correlation anchors for
// every resolvable backend-service/backend-bucket reference, and the typed
// edges from the map's own defaultService plus each pathMatcher's
// defaultService and pathRules[].service. Raw host patterns
// (hostRules[].hosts) and raw path patterns (pathMatchers[].pathRules[].paths)
// are never decoded — only resolvable backend references and bounded counts
// leave the parser.
func extractURLMap(ctx ExtractContext) (AttributeExtraction, error) {
	var data urlMapData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode url map data: %w", err)
	}

	attrs := urlMapAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if name, assetType := urlMapBackendEdge(data.DefaultService, ctx.ProjectID); name != "" {
		anchors = append(anchors, name)
		rels = append(rels, urlMapEdge(ctx, relationshipTypeURLMapDefaultService, name, assetType))
	}

	for _, pm := range data.PathMatchers {
		if name, assetType := urlMapBackendEdge(pm.DefaultService, ctx.ProjectID); name != "" {
			anchors = append(anchors, name)
			rels = append(rels, urlMapEdge(ctx, relationshipTypeURLMapPathMatcherService, name, assetType))
		}
		for _, pr := range pm.PathRules {
			if name, assetType := urlMapBackendEdge(pr.Service, ctx.ProjectID); name != "" {
				anchors = append(anchors, name)
				rels = append(rels, urlMapEdge(ctx, relationshipTypeURLMapPathRuleService, name, assetType))
			}
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// urlMapAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture. hostRules and pathMatchers are reduced to
// bounded counts; the raw host/path patterns they carry are routing logic
// that never leaves the parser.
func urlMapAttributes(data urlMapData) map[string]any {
	attrs := map[string]any{}
	if n := len(data.HostRules); n > 0 {
		attrs["host_rule_count"] = n
	}
	if n := len(data.PathMatchers); n > 0 {
		attrs["path_matcher_count"] = n
	}
	if n := urlMapPathRuleCount(data.PathMatchers); n > 0 {
		attrs["path_rule_count"] = n
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// urlMapPathRuleCount sums pathRules[] entries across every pathMatcher, so
// the attribute set carries the total bounded rule count without decoding any
// individual path pattern.
func urlMapPathRuleCount(matchers []urlMapPathMatcherData) int {
	total := 0
	for _, pm := range matchers {
		total += len(pm.PathRules)
	}
	return total
}

// urlMapBackendEdge resolves a URL Map backend-service reference (a full
// self-link or partial path) into its CAI full resource name and asset type.
// It recognizes both compute.googleapis.com/BackendService (the common case)
// and compute.googleapis.com/BackendBucket (a CDN/static-content default
// service) via the resolved path's resource segment. It returns a blank name
// for an empty, unrecognized, or unresolvable reference, so the caller emits
// no edge and no anchor for an ambiguous backend.
func urlMapBackendEdge(ref, sourceProjectID string) (fullName, assetType string) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", ""
	}
	name := computeFullResourceNameFromSelfLink(trimmed, sourceProjectID)
	if name == "" {
		return "", ""
	}
	switch {
	case strings.Contains(name, "/backendServices/"):
		return name, assetTypeComputeBackendService
	case strings.Contains(name, "/backendBuckets/"):
		return name, assetTypeComputeBackendBucket
	default:
		return "", ""
	}
}

// urlMapEdge builds a supported typed relationship observation rooted at the
// URL map.
func urlMapEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
