// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeAlloyDBCluster is the Cloud Asset Inventory asset type for an
// AlloyDB Cluster. assetTypeComputeNetwork, assetTypeKMSCryptoKey, and
// cloudKMSResourceNamePrefix are shared constants declared by the Compute
// Network and BigQuery Table extractors in this package and reused here for
// the network and CMEK edges.
const assetTypeAlloyDBCluster = "alloydb.googleapis.com/Cluster"

// Bounded provider relationship types for the AlloyDB Cluster edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	relationshipTypeAlloyDBClusterInNetwork         = "alloydb_cluster_in_network"
	relationshipTypeAlloyDBClusterEncryptedByKMSKey = "alloydb_cluster_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeAlloyDBCluster, extractAlloyDBCluster)
}

// alloyDBClusterData is the bounded view of a CAI alloydb.googleapis.com/Cluster
// resource.data blob, matching the AlloyDB v1 discovery document's Cluster
// resource. `initialUser` (username/password, input-only at cluster-creation
// time) is intentionally never decoded — no field of it is ever added to this
// struct — so a database credential can never reach the extraction output even
// if a CAI snapshot were to carry one. `encryptionInfo.kmsKeyVersions` is a
// data-plane key-version identifier list, not a control-plane field useful for
// Terraform/drift/correlation, and is left undecoded; only the bounded
// `encryptionType` posture enum is kept from that sub-message.
type alloyDBClusterData struct {
	DisplayName      string `json:"displayName"`
	UID              string `json:"uid"`
	State            string `json:"state"`
	ClusterType      string `json:"clusterType"`
	DatabaseVersion  string `json:"databaseVersion"`
	SubscriptionType string `json:"subscriptionType"`
	CreateTime       string `json:"createTime"`
	// Network is the deprecated top-level field (superseded by
	// NetworkConfig.Network); it is only consulted when NetworkConfig carries
	// no network reference.
	Network       string `json:"network"`
	NetworkConfig *struct {
		Network string `json:"network"`
	} `json:"networkConfig"`
	EncryptionConfig *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfig"`
	EncryptionInfo *struct {
		EncryptionType string `json:"encryptionType"`
	} `json:"encryptionInfo"`
	AutomatedBackupPolicy *struct {
		Enabled            *bool  `json:"enabled"`
		Location           string `json:"location"`
		BackupWindow       string `json:"backupWindow"`
		TimeBasedRetention *struct {
			RetentionPeriod string `json:"retentionPeriod"`
		} `json:"timeBasedRetention"`
		QuantityBasedRetention *struct {
			Count json.RawMessage `json:"count"`
		} `json:"quantityBasedRetention"`
	} `json:"automatedBackupPolicy"`
	ContinuousBackupConfig *struct {
		Enabled            *bool           `json:"enabled"`
		RecoveryWindowDays json.RawMessage `json:"recoveryWindowDays"`
	} `json:"continuousBackupConfig"`
}

// extractAlloyDBCluster extracts bounded, redaction-safe typed depth for one
// AlloyDB Cluster CAI asset. It returns the Terraform/drift/monitoring
// attribute set (display name, uid, state, cluster type, database version,
// subscription type, creation time, KMS key name, encryption type, automated
// and continuous backup posture); the VPC network and CMEK CryptoKey resource
// names as cross-source correlation anchors; and the typed network and
// encryption edges. The network reference's project segment is normalized from
// a numeric project number to the cluster's own project id before the edge is
// built (see alloyDBClusterNormalizeNetworkProject), since AlloyDB v1 reports
// networkConfig.network with the numeric project number while Cloud Asset
// Inventory names Compute Network assets with the project id. initialUser
// (including any password) is never decoded, so no credential material can
// ever reach the output.
func extractAlloyDBCluster(ctx ExtractContext) (AttributeExtraction, error) {
	var data alloyDBClusterData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode alloydb cluster data: %w", err)
	}

	attrs := alloyDBClusterAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if networkRef := alloyDBClusterNetworkReference(data); networkRef != "" {
		normalizedRef := alloyDBClusterNormalizeNetworkProject(networkRef, ctx.ProjectID)
		if networkName := computeFullResourceNameFromSelfLink(normalizedRef, ctx.ProjectID); networkName != "" {
			anchors = append(anchors, networkName)
			rels = append(rels, alloyDBClusterEdge(ctx, relationshipTypeAlloyDBClusterInNetwork, networkName, assetTypeComputeNetwork))
		}
	}

	if data.EncryptionConfig != nil {
		if kms := strings.TrimSpace(data.EncryptionConfig.KMSKeyName); kms != "" {
			if kmsName := cmekKeyFullResourceName(kms); kmsName != "" {
				attrs["kms_key_name"] = kms
				anchors = append(anchors, kmsName)
				rels = append(rels, alloyDBClusterEdge(ctx, relationshipTypeAlloyDBClusterEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
			}
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// alloyDBClusterNetworkReference returns the effective VPC network reference
// for the cluster, preferring networkConfig.network (the current field) over
// the deprecated top-level network field per the discovery document's
// deprecation note.
func alloyDBClusterNetworkReference(data alloyDBClusterData) string {
	if data.NetworkConfig != nil {
		if v := strings.TrimSpace(data.NetworkConfig.Network); v != "" {
			return v
		}
	}
	return strings.TrimSpace(data.Network)
}

// alloyDBClusterNormalizeNetworkProject rewrites a project-qualified network
// reference's project segment from a numeric project number to sourceProjectID
// (the cluster's own project id) when that segment is purely numeric. The
// AlloyDB v1 discovery document specifies networkConfig.network in the form
// "projects/{project_number}/global/networks/{network_id}" — the numeric
// project number, not the project id — while Cloud Asset Inventory names
// Compute Network assets with "projects/{project_id}/...". Left unnormalized,
// a numeric-project reference would resolve to a full resource name that never
// matches the collected Network node's own CAI identity, silently dropping the
// edge. The network is documented to always belong to the same project as the
// cluster, so the cluster's own project id (parsed from its CAI full resource
// name) is the correct substitution. A reference whose project segment is not
// purely numeric (already a project id) is returned unchanged; a reference
// with no "projects/" segment at all (a project-less partial or an already
// CAI-prefixed full name) is also returned unchanged, since
// computeFullResourceNameFromSelfLink resolves those shapes on its own.
func alloyDBClusterNormalizeNetworkProject(ref, sourceProjectID string) string {
	const marker = "projects/"
	idx := strings.Index(ref, marker)
	if idx < 0 {
		return ref
	}
	afterMarker := ref[idx+len(marker):]
	projectSegment, rest, ok := strings.Cut(afterMarker, "/")
	if !ok || projectSegment == "" || !isDigitsOnly(projectSegment) {
		return ref
	}
	project := strings.TrimSpace(sourceProjectID)
	if project == "" {
		return ref
	}
	return ref[:idx] + marker + project + "/" + rest
}

// isDigitsOnly reports whether s is non-empty and every rune is an ASCII
// digit, the shape of a GCP numeric project number.
func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// alloyDBClusterAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture.
func alloyDBClusterAttributes(data alloyDBClusterData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v := strings.TrimSpace(data.UID); v != "" {
		attrs["uid"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.ClusterType); v != "" {
		attrs["cluster_type"] = v
	}
	if v := strings.TrimSpace(data.DatabaseVersion); v != "" {
		attrs["database_version"] = v
	}
	if v := strings.TrimSpace(data.SubscriptionType); v != "" {
		attrs["subscription_type"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if data.EncryptionInfo != nil {
		if v := strings.TrimSpace(data.EncryptionInfo.EncryptionType); v != "" {
			attrs["encryption_type"] = v
		}
	}
	if abp := data.AutomatedBackupPolicy; abp != nil {
		if abp.Enabled != nil {
			attrs["automated_backup_enabled"] = *abp.Enabled
		}
		if v := strings.TrimSpace(abp.Location); v != "" {
			attrs["automated_backup_location"] = v
		}
		if v := strings.TrimSpace(abp.BackupWindow); v != "" {
			attrs["automated_backup_window"] = v
		}
		if tbr := abp.TimeBasedRetention; tbr != nil {
			if v := strings.TrimSpace(tbr.RetentionPeriod); v != "" {
				attrs["automated_backup_retention_period"] = v
			}
		} else if qbr := abp.QuantityBasedRetention; qbr != nil {
			if v, ok := parseFlexibleInt64(qbr.Count); ok {
				attrs["automated_backup_retention_count"] = v
			}
		}
	}
	if cbc := data.ContinuousBackupConfig; cbc != nil {
		if cbc.Enabled != nil {
			attrs["continuous_backup_enabled"] = *cbc.Enabled
		}
		if v, ok := parseFlexibleInt64(cbc.RecoveryWindowDays); ok {
			attrs["continuous_backup_recovery_window_days"] = v
		}
	}
	return attrs
}

// alloyDBClusterEdge builds a supported typed relationship observation rooted
// at the AlloyDB cluster.
func alloyDBClusterEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
