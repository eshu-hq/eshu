// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeFilestoreInstance is the Cloud Asset Inventory asset type for a
// Filestore Instance. assetTypeComputeNetwork and assetTypeKMSCryptoKey are
// shared constants declared by the Compute Network and BigQuery Table
// extractors in this package and reused here for the authorized-network and
// CMEK edges.
const assetTypeFilestoreInstance = "file.googleapis.com/Instance"

// Bounded provider relationship types for the Filestore Instance edges carried
// on gcp_cloud_relationship facts. The reducer materializes each edge only
// when both endpoints resolve exactly. relationshipTypeFilestoreInstanceInNetwork
// is emitted once per entry in the instance's networks array, since a
// Filestore Instance can be attached to more than one VPC network.
const (
	relationshipTypeFilestoreInstanceInNetwork         = "filestore_instance_in_network"
	relationshipTypeFilestoreInstanceEncryptedByKMSKey = "filestore_instance_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeFilestoreInstance, extractFilestoreInstance)
}

// filestoreInstanceData is the bounded view of a CAI
// file.googleapis.com/Instance resource.data blob. Only control-plane
// metadata, posture flags, and resource identifiers are decoded.
// filestoreNetworkData.ReservedIPRange is intentionally NOT a struct field:
// per the GCP collector contract Payload Boundaries, no CIDR or IP range ever
// reaches the parser output, so the reserved range is never decoded into Go
// memory at all. fileShares[].capacityGb is a size/quota posture value, not a
// data-plane locator, so it is safe to keep, but only a bounded share count is
// kept in the attribute map — per-share capacity is a scalability concern for
// an unbounded fileShares array, not a redaction concern.
type filestoreInstanceData struct {
	State      string                   `json:"state"`
	Tier       string                   `json:"tier"`
	CreateTime string                   `json:"createTime"`
	KMSKeyName string                   `json:"kmsKeyName"`
	Labels     map[string]string        `json:"labels"`
	FileShares []filestoreFileShareData `json:"fileShares"`
	Networks   []filestoreNetworkData   `json:"networks"`
}

// filestoreFileShareData is the bounded view of one entry in a Filestore
// Instance's fileShares array. Only the presence of the entry is used (to
// build a bounded count); no per-share name or capacity value is persisted,
// since fileShares is an unbounded, caller-controlled array.
type filestoreFileShareData struct {
	Name       string          `json:"name"`
	CapacityGb json.RawMessage `json:"capacityGb"`
}

// filestoreNetworkData is the bounded view of one entry in a Filestore
// Instance's networks array. Network is a bare short VPC network name or a
// project-qualified partial reference (never a full URL, per the Filestore
// API), resolved to a CAI full resource name for the network edge.
// ConnectMode is a control-plane posture value (e.g. DIRECT_PEERING,
// PRIVATE_SERVICE_ACCESS) and is kept; Modes (IP version) and ReservedIpRange
// are intentionally not decoded — the latter is a CIDR value and must never
// reach a fact per the GCP collector contract.
type filestoreNetworkData struct {
	Network     string `json:"network"`
	ConnectMode string `json:"connectMode"`
}

// extractFilestoreInstance extracts bounded, redaction-safe typed depth for
// one Filestore Instance CAI asset. It returns the Terraform/drift/monitoring
// attribute set (state, tier, creation time, bounded file-share count, the
// first network entry's connect mode, CMEK key name, bounded label count);
// each attached VPC Network and the CMEK CryptoKey resource names as
// cross-source correlation anchors; and one typed network edge per networks
// entry plus the CMEK encryption edge. No reservedIpRange, network mode, file
// share name/capacity, or label entry ever reaches this extractor's typed-depth
// output; the labels themselves are still captured and value-fingerprinted per
// the redaction policy by the collector's shared label path, this extractor
// simply surfaces only a bounded label_count in typed depth.
func extractFilestoreInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data filestoreInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode filestore instance data: %w", err)
	}

	attrs := filestoreInstanceAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	for _, network := range data.Networks {
		networkName := filestoreNetworkFullName(network.Network, ctx.ProjectID)
		if networkName == "" {
			continue
		}
		anchors = append(anchors, networkName)
		rels = append(rels, filestoreInstanceEdge(ctx, relationshipTypeFilestoreInstanceInNetwork, networkName, assetTypeComputeNetwork))
	}

	if kmsName := cmekKeyFullResourceName(data.KMSKeyName); kmsName != "" {
		attrs["kms_key_name"] = strings.TrimPrefix(kmsName, cloudKMSResourceNamePrefix)
		anchors = append(anchors, kmsName)
		rels = append(rels, filestoreInstanceEdge(ctx, relationshipTypeFilestoreInstanceEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// filestoreInstanceAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture. The connect mode is taken from the
// first networks entry only — Filestore instances typically attach to exactly
// one VPC network, and a single scalar connect-mode attribute avoids
// unbounded per-network attribute keys for the (rare) multi-network case,
// which is still fully represented via the per-network edges.
func filestoreInstanceAttributes(data filestoreInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.Tier); v != "" {
		attrs["tier"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if len(data.FileShares) > 0 {
		attrs["file_share_count"] = len(data.FileShares)
	}
	if len(data.Networks) > 0 {
		if v := strings.TrimSpace(data.Networks[0].ConnectMode); v != "" {
			attrs["connect_mode"] = v
		}
	}
	if len(data.Labels) > 0 {
		attrs["label_count"] = len(data.Labels)
	}
	return attrs
}

// filestoreNetworkFullName resolves a Filestore Instance network reference to
// its CAI full resource name. The Filestore API reports the network as a bare
// short name (e.g. "default") or a project-qualified partial
// (projects/p/global/networks/n) — never a full compute API URL, unlike
// Compute-family resources. A bare name is promoted to the project-less
// global partial before resolution against the instance's project, mirroring
// the GKE Cluster extractor's own network-reference handling.
func filestoreNetworkFullName(ref, projectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		trimmed = "global/networks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// filestoreInstanceEdge builds a supported typed relationship observation
// rooted at the Filestore instance.
func filestoreInstanceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
