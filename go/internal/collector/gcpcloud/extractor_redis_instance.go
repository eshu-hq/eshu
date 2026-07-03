// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeRedisInstance is the Cloud Asset Inventory asset type for a
// Memorystore for Redis Instance. assetTypeComputeNetwork and
// assetTypeKMSCryptoKey are shared constants declared by the Compute Network
// and BigQuery Table extractors in this package and reused here for the
// authorized-network and CMEK edges.
const assetTypeRedisInstance = "redis.googleapis.com/Instance"

// Bounded provider relationship types for the Memorystore Redis Instance edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly.
const (
	relationshipTypeRedisInstanceInNetwork         = "redis_instance_in_network"
	relationshipTypeRedisInstanceEncryptedByKMSKey = "redis_instance_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeRedisInstance, extractRedisInstance)
}

// redisInstanceData is the bounded view of a CAI redis.googleapis.com/Instance
// resource.data blob. Only control-plane metadata, posture flags, and resource
// identifiers are decoded. Connection-plane locators — host, port,
// readEndpoint, readEndpointPort, reservedIpRange, and secondaryIpRange — are
// never decoded, since they are IP addresses, ports, or CIDR ranges rather than
// resource identities. memorySizeGb and replicaCount arrive as JSON numbers, so
// they are decoded as raw JSON and normalized the same way Cloud SQL Instance
// and Persistent Disk normalize their own numeric fields across API
// number/string variance.
type redisInstanceData struct {
	LocationID            string          `json:"locationId"`
	RedisVersion          string          `json:"redisVersion"`
	Tier                  string          `json:"tier"`
	MemorySizeGb          json.RawMessage `json:"memorySizeGb"`
	AuthorizedNetwork     string          `json:"authorizedNetwork"`
	ConnectMode           string          `json:"connectMode"`
	TransitEncryptionMode string          `json:"transitEncryptionMode"`
	AuthEnabled           *bool           `json:"authEnabled"`
	State                 string          `json:"state"`
	CreateTime            string          `json:"createTime"`
	ReplicaCount          json.RawMessage `json:"replicaCount"`
	ReadReplicasMode      string          `json:"readReplicasMode"`
	CustomerManagedKey    string          `json:"customerManagedKey"`
	PersistenceConfig     *struct {
		PersistenceMode string `json:"persistenceMode"`
	} `json:"persistenceConfig"`
}

// extractRedisInstance extracts bounded, redaction-safe typed depth for one
// Memorystore for Redis Instance CAI asset. It returns the Terraform/drift/
// monitoring attribute set (location id, Redis version, tier, memory size,
// connect mode, transit-encryption mode, auth-enabled posture, state, creation
// time, replica count, read-replicas mode, CMEK key name, persistence mode);
// the authorized Compute Network and CMEK CryptoKey resource names as
// cross-source correlation anchors; and the typed network and encryption
// edges. No host, port, read-endpoint, or IP range (reservedIpRange,
// secondaryIpRange) ever reaches the output.
func extractRedisInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data redisInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode redis instance data: %w", err)
	}

	attrs := redisInstanceAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if networkName := computeFullResourceNameFromSelfLink(data.AuthorizedNetwork, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, redisInstanceEdge(ctx, relationshipTypeRedisInstanceInNetwork, networkName, assetTypeComputeNetwork))
	}

	if kms := strings.TrimSpace(data.CustomerManagedKey); kms != "" {
		if kmsName := redisInstanceKMSKeyFullName(kms); kmsName != "" {
			attrs["customer_managed_key"] = strings.TrimPrefix(kmsName, cloudKMSResourceNamePrefix)
			anchors = append(anchors, kmsName)
			rels = append(rels, redisInstanceEdge(ctx, relationshipTypeRedisInstanceEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// redisInstanceAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example a false auth-enabled flag that was
// simply not reported).
func redisInstanceAttributes(data redisInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.LocationID); v != "" {
		attrs["location_id"] = v
	}
	if v := strings.TrimSpace(data.RedisVersion); v != "" {
		attrs["redis_version"] = v
	}
	if v := strings.TrimSpace(data.Tier); v != "" {
		attrs["tier"] = v
	}
	if v, ok := parseFlexibleInt64(data.MemorySizeGb); ok {
		attrs["memory_size_gb"] = v
	}
	if v := strings.TrimSpace(data.ConnectMode); v != "" {
		attrs["connect_mode"] = v
	}
	if v := strings.TrimSpace(data.TransitEncryptionMode); v != "" {
		attrs["transit_encryption_mode"] = v
	}
	if data.AuthEnabled != nil {
		attrs["auth_enabled"] = *data.AuthEnabled
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := parseFlexibleInt64(data.ReplicaCount); ok {
		attrs["replica_count"] = v
	}
	if v := strings.TrimSpace(data.ReadReplicasMode); v != "" {
		attrs["read_replicas_mode"] = v
	}
	if data.PersistenceConfig != nil {
		if v := strings.TrimSpace(data.PersistenceConfig.PersistenceMode); v != "" {
			attrs["persistence_mode"] = v
		}
	}
	return attrs
}

// redisInstanceKMSKeyFullName builds the CAI CryptoKey full resource name from
// customerManagedKey, which the Memorystore API documents as a KMS key
// reference without a fixed prefix convention. An already CAI-prefixed
// ("//cloudkms.googleapis.com/...") value is returned unchanged so the prefix
// is never doubled; a bare relative name is prefixed as-is, mirroring the
// Dataproc Cluster and Cloud Storage Bucket CMEK normalization. It returns ""
// only for a blank reference.
func redisInstanceKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// redisInstanceEdge builds a supported typed relationship observation rooted
// at the Memorystore Redis instance.
func redisInstanceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
