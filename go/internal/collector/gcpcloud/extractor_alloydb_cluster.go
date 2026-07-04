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
// encryption edges. The network reference's project segment is left exactly as
// AlloyDB reports it (see alloyDBClusterNetworkFullName): AlloyDB supports
// Shared VPC, where a cluster's own project (a Shared VPC service project) can
// reference a network owned by a different host project, so a numeric project
// segment cannot be safely assumed to be the cluster's own project number and
// rewritten to the cluster's project id — doing so could fabricate a wrong
// edge to a same-named network that happens to exist in the cluster's project.
// A numeric-project reference that does not match any collected Compute
// Network's project-id-keyed CAI identity simply yields no edge (safe) rather
// than a wrong one. initialUser (including any password) is never decoded, so
// no credential material can ever reach the output.
func extractAlloyDBCluster(ctx ExtractContext) (AttributeExtraction, error) {
	var data alloyDBClusterData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode alloydb cluster data: %w", err)
	}

	attrs := alloyDBClusterAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if networkRef := alloyDBClusterNetworkReference(data); networkRef != "" {
		if networkName := alloyDBClusterNetworkFullName(networkRef, ctx.ProjectID); networkName != "" {
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

// alloyDBClusterNetworkFullName derives the Compute Network CAI full resource
// name from a networkConfig.network (or deprecated top-level network)
// reference. AlloyDB reports this reference with a numeric project number in
// the common non-Shared-VPC case per the v1 discovery document, but AlloyDB
// also supports Shared VPC, where the cluster's own project (a service
// project) references a network owned by a different host project — so the
// reference's project segment cannot be assumed to be the cluster's own
// project number.
//
// A project-qualified reference (projects/<segment>/global/networks/<id>,
// whether bare or already CAI-prefixed with //compute.googleapis.com/) is
// therefore passed through with its project segment exactly as reported,
// never rewritten to sourceProjectID: rewriting a numeric segment would risk
// fabricating an edge to a same-named network that happens to exist in the
// cluster's own project, which is worse than the edge silently not resolving
// (Cloud Asset Inventory names Compute Network assets with the project id, so
// an unresolved numeric segment simply yields no edge). Only a project-less
// partial (global/networks/<id>, with no "projects/" segment at all) is
// resolved against sourceProjectID, since that shape is unambiguous — GCP APIs
// only omit the project segment for a same-project reference.
func alloyDBClusterNetworkFullName(ref, sourceProjectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, computeResourceNamePrefix) {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "projects/") {
		return computeResourceNamePrefix + trimmed
	}
	if strings.HasPrefix(trimmed, "global/networks/") {
		project := strings.TrimSpace(sourceProjectID)
		if project == "" {
			return ""
		}
		return computeResourceNamePrefix + "projects/" + project + "/" + trimmed
	}
	return ""
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
