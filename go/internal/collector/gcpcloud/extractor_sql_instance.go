// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeSQLInstance is the Cloud Asset Inventory asset type for a Cloud SQL
// Instance. assetTypeComputeNetwork and assetTypeKMSCryptoKey are shared
// constants declared by the Compute Network and BigQuery Table extractors in
// this package and reused here for the private-network and CMEK edges.
const assetTypeSQLInstance = "sqladmin.googleapis.com/Instance"

// sqlInstanceResourceNamePrefix is the CAI full-resource-name prefix for a Cloud
// SQL Instance, used to build the master/replica edge endpoints from a bare
// "projects/<p>/instances/<i>" reference (the sqladmin API convention for
// masterInstanceName and replicaNames).
const sqlInstanceResourceNamePrefix = "//sqladmin.googleapis.com/"

// Bounded provider relationship types for the Cloud SQL Instance edges carried
// on gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	relationshipTypeSQLInstanceInNetwork         = "sql_instance_in_network"
	relationshipTypeSQLInstanceEncryptedByKMSKey = "sql_instance_encrypted_by_kms_key"
	relationshipTypeSQLInstanceHasReplica        = "sql_instance_has_replica"
	relationshipTypeSQLInstanceReplicaOf         = "sql_instance_replica_of"
)

func init() {
	RegisterAssetExtractor(assetTypeSQLInstance, extractSQLInstance)
}

// sqlInstanceData is the bounded view of a CAI sqladmin.googleapis.com/Instance
// resource.data blob. Only control-plane metadata and resource identifiers are
// decoded. IP address values (ipAddresses[].ipAddress and
// authorizedNetworks[].value, which is a CIDR) are never decoded — only
// ipv4Enabled (a boolean posture) and the authorized-network count are kept, so
// no address or CIDR reaches the parser output. dataDiskSizeGb and
// transactionLogRetentionDays arrive as JSON numbers or strings depending on API
// surface/version, so they are decoded as raw JSON and normalized.
type sqlInstanceData struct {
	DatabaseVersion    string   `json:"databaseVersion"`
	Region             string   `json:"region"`
	State              string   `json:"state"`
	InstanceType       string   `json:"instanceType"`
	MasterInstanceName string   `json:"masterInstanceName"`
	ReplicaNames       []string `json:"replicaNames"`
	CreateTime         string   `json:"createTime"`
	Settings           *struct {
		Tier             string          `json:"tier"`
		AvailabilityType string          `json:"availabilityType"`
		DataDiskSizeGb   json.RawMessage `json:"dataDiskSizeGb"`
		IPConfiguration  *struct {
			IPv4Enabled *bool `json:"ipv4Enabled"`
			// PrivateNetwork is a resource reference (VPC self-link or partial
			// path), not an address, so it is decoded and kept as an edge target.
			PrivateNetwork string `json:"privateNetwork"`
			// AuthorizedNetworks entries carry a CIDR in `value`; only the entry
			// count is kept, never the CIDR or the network's `name` label.
			AuthorizedNetworks []json.RawMessage `json:"authorizedNetworks"`
			SSLMode            string            `json:"sslMode"`
		} `json:"ipConfiguration"`
		BackupConfiguration *struct {
			Enabled                     *bool           `json:"enabled"`
			PointInTimeRecoveryEnabled  *bool           `json:"pointInTimeRecoveryEnabled"`
			TransactionLogRetentionDays json.RawMessage `json:"transactionLogRetentionDays"`
		} `json:"backupConfiguration"`
	} `json:"settings"`
	DiskEncryptionConfiguration *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"diskEncryptionConfiguration"`
}

// extractSQLInstance extracts bounded, redaction-safe typed depth for one Cloud
// SQL Instance CAI asset. It returns the Terraform/drift/monitoring attribute
// set (database version, region, state, instance type, tier, availability
// type, disk size, public-IP posture, SSL mode, authorized-network count,
// backup/PITR posture, KMS key name, replica count, creation time); the private
// network, CMEK CryptoKey, and replica/master resource names as cross-source
// correlation anchors; and the typed private-network, encryption, and
// replica-topology edges. No IP address or authorized-network CIDR/name ever
// reaches the output — ipv4Enabled and the authorized-network count are kept
// instead.
func extractSQLInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data sqlInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode sql instance data: %w", err)
	}

	attrs := sqlInstanceAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if data.Settings != nil && data.Settings.IPConfiguration != nil {
		if networkName := sqlInstancePrivateNetworkFullName(data.Settings.IPConfiguration.PrivateNetwork, ctx.ProjectID); networkName != "" {
			anchors = append(anchors, networkName)
			rels = append(rels, sqlInstanceEdge(ctx, relationshipTypeSQLInstanceInNetwork, networkName, assetTypeComputeNetwork))
		}
	}

	if data.DiskEncryptionConfiguration != nil {
		if kms := strings.TrimSpace(data.DiskEncryptionConfiguration.KMSKeyName); kms != "" {
			attrs["kms_key_name"] = kms
			if kmsName := sqlInstanceKMSKeyFullName(kms); kmsName != "" {
				anchors = append(anchors, kmsName)
				rels = append(rels, sqlInstanceEdge(ctx, relationshipTypeSQLInstanceEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
			}
		}
	}

	if masterName := sqlInstanceReferenceFullName(data.MasterInstanceName, ctx.ProjectID); masterName != "" {
		anchors = append(anchors, masterName)
		rels = append(rels, sqlInstanceEdge(ctx, relationshipTypeSQLInstanceReplicaOf, masterName, assetTypeSQLInstance))
	}

	replicaCount := 0
	for _, replica := range data.ReplicaNames {
		replicaName := sqlInstanceReferenceFullName(replica, ctx.ProjectID)
		if replicaName == "" {
			continue
		}
		replicaCount++
		anchors = append(anchors, replicaName)
		rels = append(rels, sqlInstanceEdge(ctx, relationshipTypeSQLInstanceHasReplica, replicaName, assetTypeSQLInstance))
	}
	if replicaCount > 0 {
		attrs["replica_count"] = replicaCount
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// sqlInstanceAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example a false public-IP flag that was
// simply not reported).
func sqlInstanceAttributes(data sqlInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DatabaseVersion); v != "" {
		attrs["database_version"] = v
	}
	if v := strings.TrimSpace(data.Region); v != "" {
		attrs["region"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.InstanceType); v != "" {
		attrs["instance_type"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if s := data.Settings; s != nil {
		if v := strings.TrimSpace(s.Tier); v != "" {
			attrs["tier"] = v
		}
		if v := strings.TrimSpace(s.AvailabilityType); v != "" {
			attrs["availability_type"] = v
		}
		if v, ok := parseFlexibleInt64(s.DataDiskSizeGb); ok {
			attrs["data_disk_size_gb"] = v
		}
		if ip := s.IPConfiguration; ip != nil {
			if ip.IPv4Enabled != nil {
				attrs["public_ip_enabled"] = *ip.IPv4Enabled
			}
			if v := strings.TrimSpace(ip.SSLMode); v != "" {
				attrs["ssl_mode"] = v
			}
			if n := len(ip.AuthorizedNetworks); n > 0 {
				attrs["authorized_network_count"] = n
			}
		}
		if bc := s.BackupConfiguration; bc != nil {
			if bc.Enabled != nil {
				attrs["backups_enabled"] = *bc.Enabled
			}
			if bc.PointInTimeRecoveryEnabled != nil {
				attrs["point_in_time_recovery_enabled"] = *bc.PointInTimeRecoveryEnabled
			}
			if v, ok := parseFlexibleInt64(bc.TransactionLogRetentionDays); ok {
				attrs["transaction_log_retention_days"] = v
			}
		}
	}
	return attrs
}

// sqlInstancePrivateNetworkFullName derives the Compute Network CAI full
// resource name from settings.ipConfiguration.privateNetwork, which the
// sqladmin API reports as a full selfLink, a project-qualified partial
// (projects/p/global/networks/n), or a project-less partial
// (global/networks/n) resolved against the instance's own project. It returns
// "" for a blank reference or a shape that does not name a Compute Network.
func sqlInstancePrivateNetworkFullName(ref, sourceProjectID string) string {
	return computeFullResourceNameFromSelfLink(ref, sourceProjectID)
}

// sqlInstanceReferenceFullName derives a Cloud SQL Instance CAI full resource
// name from a masterInstanceName/replicaNames entry. The sqladmin API reports
// this either as a project-qualified "projects/<p>/instances/<i>" reference or,
// in the common same-project case, as a bare instance name with no project
// qualifier at all (per the Cloud SQL Admin API's instance `name` field
// convention, which omits the project ID) — that bare form is resolved against
// sourceProjectID. It returns "" for a blank reference, a shape that does not
// name a Cloud SQL instance, or a bare name with no source project to qualify
// it against.
func sqlInstanceReferenceFullName(ref, sourceProjectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, sqlInstanceResourceNamePrefix) {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "projects/") && strings.Contains(trimmed, "/instances/") {
		return sqlInstanceResourceNamePrefix + trimmed
	}
	// Bare instance name (no "/" at all): qualify against the source project.
	if !strings.Contains(trimmed, "/") {
		project := strings.TrimSpace(sourceProjectID)
		if project == "" {
			return ""
		}
		return sqlInstanceResourceNamePrefix + "projects/" + project + "/instances/" + trimmed
	}
	return ""
}

// sqlInstanceKMSKeyFullName derives the Cloud KMS CryptoKey CAI full resource
// name from diskEncryptionConfiguration.kmsKeyName, which the sqladmin API may
// report as a bare "projects/.../locations/.../keyRings/.../cryptoKeys/..."
// relative name or as an already CAI-prefixed
// "//cloudkms.googleapis.com/..." full resource name. It layers a bare-name
// shape gate on top of the shared strict CMEK normalization
// (cmekKeyFullResourceName): a bare value is accepted only when it matches the
// expected CryptoKey path shape, so a malformed bare key name never becomes an
// edge endpoint or anchor. An absolute name is delegated to the shared helper,
// which keeps a Cloud KMS full name unchanged and rejects any other service
// domain. It returns "" for a blank reference or an unrecognized bare shape.
func sqlInstanceKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	// A bare (non-absolute) reference must match the full CryptoKey path shape
	// before the shared helper prefixes it; the shared helper alone would prefix
	// any bare string, so the shape gate stays here to preserve sqladmin's
	// stricter contract. An absolute name is delegated straight to the shared
	// helper, which enforces the Cloud KMS domain.
	if !strings.HasPrefix(trimmed, "//") && !isSQLCryptoKeyBareShape(trimmed) {
		return ""
	}
	return cmekKeyFullResourceName(trimmed)
}

// isSQLCryptoKeyBareShape reports whether a bare (non-absolute) kmsKeyName has
// the full CryptoKey path shape the sqladmin API is documented to emit
// (projects/.../locations/.../keyRings/.../cryptoKeys/...). It gates the bare
// branch of sqlInstanceKMSKeyFullName so a malformed relative name is rejected
// rather than blindly prefixed into a bogus CryptoKey endpoint.
func isSQLCryptoKeyBareShape(bareName string) bool {
	return strings.HasPrefix(bareName, "projects/") &&
		strings.Contains(bareName, "/locations/") &&
		strings.Contains(bareName, "/keyRings/") &&
		strings.Contains(bareName, "/cryptoKeys/")
}

// sqlInstanceEdge builds a supported typed relationship observation rooted at
// the Cloud SQL instance.
func sqlInstanceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
