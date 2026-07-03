// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeBigtableCluster is the Cloud Asset Inventory asset type for a Cloud
// Bigtable Cluster. assetTypeBigtableInstance is declared by the sibling
// Bigtable Instance extractor and reused here for the parent-instance edge;
// assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix are declared by the
// BigQuery Table extractor and reused here for the CMEK edge.
const assetTypeBigtableCluster = "bigtableadmin.googleapis.com/Cluster"

// bigtableClustersMarker separates a Bigtable Cluster full resource name from
// its parent Instance full resource name. A cluster resource name is
// ".../instances/<instance>/clusters/<cluster>", so trimming from this marker
// yields the parent Instance identity.
const bigtableClustersMarker = "/clusters/"

// Bounded provider relationship types for Bigtable Cluster edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	relationshipTypeBigtableClusterInInstance     = "bigtable_cluster_in_instance"
	relationshipTypeBigtableClusterEncryptedByKMS = "bigtable_cluster_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeBigtableCluster, extractBigtableCluster)
}

// bigtableClusterData is the bounded view of a CAI
// bigtableadmin.googleapis.com/Cluster resource.data blob. Only redaction-safe
// control-plane metadata, posture fields, and the CMEK key reference are
// decoded. serveNodes arrives as a JSON number, decoded as raw JSON and
// normalized across API number/string variance the same way other extractors
// in this package normalize their numeric fields. No data-plane content
// (table schemas, row data) is decoded.
type bigtableClusterData struct {
	Location           string          `json:"location"`
	State              string          `json:"state"`
	ServeNodes         json.RawMessage `json:"serveNodes"`
	NodeScalingFactor  string          `json:"nodeScalingFactor"`
	DefaultStorageType string          `json:"defaultStorageType"`
	EncryptionConfig   *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfig"`
}

// extractBigtableCluster extracts bounded, redaction-safe typed depth for one
// Cloud Bigtable Cluster CAI asset. It returns the Terraform/drift/monitoring
// attribute set (location, state, serve nodes, node scaling factor, default
// storage type, and the CMEK key name when present); the parent Instance and
// CMEK CryptoKey resource names as cross-source correlation anchors; and the
// typed bigtable_cluster_in_instance edge to the parent Instance (derived from
// the cluster's own resource-name path, since a Cluster resource name embeds
// its parent) plus the bigtable_cluster_encrypted_by_kms_key edge to the CMEK
// CryptoKey. No table schema or row data ever reaches the output.
func extractBigtableCluster(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigtableClusterData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigtable cluster data: %w", err)
	}

	attrs := bigtableClusterAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if parent := bigtableParentInstanceFullName(ctx.FullResourceName); parent != "" {
		anchors = append(anchors, parent)
		rels = append(rels, bigtableClusterEdge(ctx, relationshipTypeBigtableClusterInInstance, parent, assetTypeBigtableInstance))
	}

	if data.EncryptionConfig != nil {
		if kms := strings.TrimSpace(data.EncryptionConfig.KMSKeyName); kms != "" {
			if kmsName := bigtableClusterKMSKeyFullName(kms); kmsName != "" {
				attrs["kms_key_name"] = strings.TrimPrefix(kmsName, cloudKMSResourceNamePrefix)
				anchors = append(anchors, kmsName)
				rels = append(rels, bigtableClusterEdge(ctx, relationshipTypeBigtableClusterEncryptedByKMS, kmsName, assetTypeKMSCryptoKey))
			}
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// bigtableClusterAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial CAI
// page does not fabricate a posture.
func bigtableClusterAttributes(data bigtableClusterData) map[string]any {
	attrs := map[string]any{}
	if v := bigtableClusterLocationShortName(data.Location); v != "" {
		attrs["location"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v, ok := parseFlexibleInt64(data.ServeNodes); ok {
		attrs["serve_nodes"] = v
	}
	if v := strings.TrimSpace(data.NodeScalingFactor); v != "" {
		attrs["node_scaling_factor"] = v
	}
	if v := strings.TrimSpace(data.DefaultStorageType); v != "" {
		attrs["default_storage_type"] = v
	}
	return attrs
}

// bigtableParentInstanceFullName derives the parent Bigtable Instance full
// resource name from a cluster's own full resource name by trimming the
// "/clusters/<cluster>" suffix. A cluster resource name is
// ".../instances/<instance>/clusters/<cluster>", so the prefix up to the
// marker is exactly the parent Instance identity. It returns "" when the name
// is blank or carries no "/clusters/" segment (a malformed or partial name),
// so no parent edge is fabricated.
func bigtableParentInstanceFullName(clusterFullName string) string {
	trimmed := strings.TrimSpace(clusterFullName)
	index := strings.LastIndex(trimmed, bigtableClustersMarker)
	if index <= 0 {
		return ""
	}
	return trimmed[:index]
}

// bigtableClusterLocationShortName reduces a cluster's location reference —
// reported as a relative resource name
// ("projects/<p>/locations/<location-id>") or, defensively, an already-bare
// location id — to the bare location id (e.g. "us-central1-b"). Only the
// location id is kept; the project segment is dropped since it is redundant
// with the cluster's own resource name and a location is not itself a
// resolvable CAI endpoint.
func bigtableClusterLocationShortName(location string) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/locations/"); idx >= 0 {
		return trimmed[idx+len("/locations/"):]
	}
	return trimmed
}

// bigtableClusterKMSKeyFullName builds the CAI CryptoKey full resource name
// from a cluster's encryptionConfig.kmsKeyName, which the Bigtable Admin API
// documents as a KMS key reference without a fixed prefix convention. An
// already CAI-prefixed ("//cloudkms.googleapis.com/...") value is returned
// unchanged so the prefix is never doubled; a bare relative name is prefixed
// as-is, mirroring the Memorystore Redis Instance and Cloud Storage Bucket
// CMEK normalization. It returns "" only for a blank reference.
func bigtableClusterKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// bigtableClusterEdge builds one typed provider relationship observation
// anchored on the cluster's CAI full resource name.
func bigtableClusterEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
