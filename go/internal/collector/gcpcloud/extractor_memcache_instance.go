// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeMemcacheInstance is the Cloud Asset Inventory asset type for a
// Memorystore for Memcached instance. assetTypeComputeNetwork is a shared
// constant declared by the Subnetwork extractor (extractor_subnetwork.go) in
// this package and reused here for the authorized-network edge.
const assetTypeMemcacheInstance = "memcache.googleapis.com/Instance"

// relationshipTypeMemcacheInstanceInNetwork is the bounded provider
// relationship type for the Memorystore Memcached instance's authorized-
// network edge, carried on a gcp_cloud_relationship fact. The reducer
// materializes the edge only when both endpoints resolve exactly.
const relationshipTypeMemcacheInstanceInNetwork = "memcache_instance_in_network"

func init() {
	RegisterAssetExtractor(assetTypeMemcacheInstance, extractMemcacheInstance)
}

// memcacheNodeConfigData is the bounded view of the CAI
// memcache.googleapis.com/Instance nodeConfig sub-object. Per-node cpuCount
// and memorySizeMb describe every node uniformly, so they are captured once
// at the instance level rather than per node.
type memcacheNodeConfigData struct {
	CPUCount     json.RawMessage `json:"cpuCount"`
	MemorySizeMb json.RawMessage `json:"memorySizeMb"`
}

// memcacheInstanceData is the bounded view of a CAI
// memcache.googleapis.com/Instance resource.data blob. Only control-plane
// metadata, posture, and resource identifiers are decoded. Connection-plane
// locators — discoveryEndpoint and each memcacheNodes[].host/port — are never
// decoded, since they are hostnames, IP addresses, or ports rather than
// resource identities. nodeCount, nodeConfig.cpuCount, and
// nodeConfig.memorySizeMb arrive as JSON numbers, so they are decoded as raw
// JSON and normalized the same way Cloud SQL Instance and Persistent Disk
// normalize their own numeric fields across API number/string variance.
type memcacheInstanceData struct {
	DisplayName                 string                  `json:"displayName"`
	AuthorizedNetwork           string                  `json:"authorizedNetwork"`
	Zones                       []string                `json:"zones"`
	NodeCount                   json.RawMessage         `json:"nodeCount"`
	NodeConfig                  *memcacheNodeConfigData `json:"nodeConfig"`
	MemcacheVersion             string                  `json:"memcacheVersion"`
	MemcacheFullVersion         string                  `json:"memcacheFullVersion"`
	CreateTime                  string                  `json:"createTime"`
	State                       string                  `json:"state"`
	MaintenanceVersion          string                  `json:"maintenanceVersion"`
	EffectiveMaintenanceVersion string                  `json:"effectiveMaintenanceVersion"`
	MemcacheNodes               []memcacheNodeData      `json:"memcacheNodes"`
}

// memcacheNodeData is the bounded view of one CAI memcacheNodes[] entry. Only
// nodeId, zone, and state fields exist on this struct's fields set that are
// decoded into the node count; host and port are never declared as fields, so
// they never reach Go memory and can never leak into a fact.
type memcacheNodeData struct {
	NodeID string `json:"nodeId"`
	Zone   string `json:"zone"`
	State  string `json:"state"`
}

// extractMemcacheInstance extracts bounded, redaction-safe typed depth for
// one Memorystore for Memcached Instance CAI asset. It returns the
// Terraform/drift/monitoring attribute set (display name, zone count, node
// count, per-node cpu count and memory size, memcache version, full version,
// creation time, state, maintenance version, effective maintenance version,
// memcache node count); the authorized Compute Network resource name as a
// cross-source correlation anchor; and the typed network edge. No
// discoveryEndpoint, node host, or node port ever reaches the output.
func extractMemcacheInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data memcacheInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode memcache instance data: %w", err)
	}

	attrs := memcacheInstanceAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if networkName := computeFullResourceNameFromSelfLink(data.AuthorizedNetwork, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, memcacheInstanceEdge(ctx, relationshipTypeMemcacheInstanceInNetwork, networkName, assetTypeComputeNetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// memcacheInstanceAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture (for example a false node count that
// was simply not reported).
func memcacheInstanceAttributes(data memcacheInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if n := len(data.Zones); n > 0 {
		attrs["zone_count"] = n
	}
	if v, ok := parseFlexibleInt64(data.NodeCount); ok {
		attrs["node_count"] = v
	}
	if data.NodeConfig != nil {
		if v, ok := parseFlexibleInt64(data.NodeConfig.CPUCount); ok {
			attrs["cpu_count"] = v
		}
		if v, ok := parseFlexibleInt64(data.NodeConfig.MemorySizeMb); ok {
			attrs["memory_size_mb"] = v
		}
	}
	if v := strings.TrimSpace(data.MemcacheVersion); v != "" {
		attrs["memcache_version"] = v
	}
	if v := strings.TrimSpace(data.MemcacheFullVersion); v != "" {
		attrs["memcache_full_version"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.MaintenanceVersion); v != "" {
		attrs["maintenance_version"] = v
	}
	if v := strings.TrimSpace(data.EffectiveMaintenanceVersion); v != "" {
		attrs["effective_maintenance_version"] = v
	}
	if n := len(data.MemcacheNodes); n > 0 {
		attrs["memcache_node_count"] = n
	}
	return attrs
}

// memcacheInstanceEdge builds a supported typed relationship observation
// rooted at the Memorystore Memcached instance.
func memcacheInstanceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
