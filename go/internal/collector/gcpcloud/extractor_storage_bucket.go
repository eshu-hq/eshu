// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bounded provider relationship types for Cloud Storage Bucket edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	// relationshipTypeStorageBucketKMSKey is the CMEK default-encryption edge from
	// the bucket to its Cloud KMS CryptoKey.
	relationshipTypeStorageBucketKMSKey = "storage_bucket_encrypted_by_kms_key"
	// relationshipTypeStorageBucketLogsToBucket is the usage-logging export edge
	// from the bucket to the destination log bucket.
	relationshipTypeStorageBucketLogsToBucket = "storage_bucket_logs_to_bucket"
)

func init() {
	RegisterAssetExtractor(assetTypeStorageBucket, extractStorageBucket)
}

// storageBucketData is the bounded view of a CAI storage.googleapis.com/Bucket
// resource.data blob (the GCS JSON API Bucket representation). Only
// redaction-safe control-plane metadata is decoded: placement, storage class,
// timestamps, uniform-bucket-level-access and public-access-prevention posture,
// the CMEK key reference, versioning, a bounded lifecycle-rule count, the usage
// logging destination bucket, and retention-policy posture. The bucket's `acl`,
// `defaultObjectAcl`, and `iamConfiguration.bucketPolicyOnly` legacy IAM policy
// fields are never decoded — per the GCP collector contract, no raw IAM policy
// JSON or member identity leaves the parser. Object contents, object names, and
// notification/pubsub configuration are data-plane or out-of-scope and are never
// read.
type storageBucketData struct {
	Location     string `json:"location"`
	LocationType string `json:"locationType"`
	StorageClass string `json:"storageClass"`
	TimeCreated  string `json:"timeCreated"`
	Updated      string `json:"updated"`
	IAMConfig    *struct {
		UniformBucketLevelAccess *struct {
			Enabled bool `json:"enabled"`
		} `json:"uniformBucketLevelAccess"`
		PublicAccessPrevention string `json:"publicAccessPrevention"`
	} `json:"iamConfiguration"`
	Encryption *struct {
		DefaultKMSKeyName string `json:"defaultKmsKeyName"`
	} `json:"encryption"`
	Versioning *struct {
		Enabled bool `json:"enabled"`
	} `json:"versioning"`
	Lifecycle *struct {
		Rule []json.RawMessage `json:"rule"`
	} `json:"lifecycle"`
	Logging *struct {
		LogBucket string `json:"logBucket"`
	} `json:"logging"`
	RetentionPolicy *struct {
		RetentionPeriod string `json:"retentionPeriod"`
		IsLocked        bool   `json:"isLocked"`
	} `json:"retentionPolicy"`
}

// extractStorageBucket extracts bounded, redaction-safe typed depth for one CAI
// Cloud Storage Bucket asset. It returns the Terraform/drift/monitoring
// attribute set (placement, storage class, timestamps, uniform-bucket-level-access
// and public-access-prevention posture, CMEK posture, versioning, a bounded
// lifecycle-rule count, and retention-policy posture), correlation anchors, and
// typed edges: the CMEK default-encryption relationship to the Cloud KMS
// CryptoKey and the usage-logging export relationship to the destination log
// bucket. The bucket's ACL/IAM policy, object contents, and notification
// configuration are never decoded — only resource identities (the KMS key name
// and the logging destination bucket name) leave the parser.
func extractStorageBucket(ctx ExtractContext) (AttributeExtraction, error) {
	var data storageBucketData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode storage bucket data: %w", err)
	}

	attrs := storageBucketAttributes(data)
	var anchors []string
	rels := make([]RelationshipObservation, 0, 2)

	if kms := storageBucketKMSKeyName(data); kms != "" {
		attrs["kms_key_name"] = kms
		kmsName := cloudKMSResourceNamePrefix + kms
		anchors = append(anchors, kmsName)
		rels = append(rels, storageBucketEdge(ctx, relationshipTypeStorageBucketKMSKey, kmsName, assetTypeKMSCryptoKey))
	}

	if logBucket := storageBucketLoggingDestination(data); logBucket != "" {
		target := storageBucketResourceNamePrefixFmt + logBucket
		anchors = append(anchors, target)
		rels = append(rels, storageBucketEdge(ctx, relationshipTypeStorageBucketLogsToBucket, target, assetTypeStorageBucket))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// storageBucketAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example an unset uniform-bucket-level-access
// or an empty lifecycle policy).
func storageBucketAttributes(data storageBucketData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Location); v != "" {
		attrs["location"] = v
	}
	if v := strings.TrimSpace(data.LocationType); v != "" {
		attrs["location_type"] = v
	}
	if v := strings.TrimSpace(data.StorageClass); v != "" {
		attrs["storage_class"] = v
	}
	if v, ok := normalizeRFC3339(data.TimeCreated); ok {
		attrs["time_created"] = v
	}
	if v, ok := normalizeRFC3339(data.Updated); ok {
		attrs["updated"] = v
	}
	if data.IAMConfig != nil {
		if data.IAMConfig.UniformBucketLevelAccess != nil {
			attrs["uniform_bucket_level_access"] = data.IAMConfig.UniformBucketLevelAccess.Enabled
		}
		if v := strings.TrimSpace(data.IAMConfig.PublicAccessPrevention); v != "" {
			attrs["public_access_prevention"] = v
		}
	}
	if data.Versioning != nil {
		attrs["versioning_enabled"] = data.Versioning.Enabled
	}
	if data.Lifecycle != nil {
		if n := len(data.Lifecycle.Rule); n > 0 {
			attrs["lifecycle_rule_count"] = n
		}
	}
	if data.RetentionPolicy != nil {
		if v, ok := parseInt64String(data.RetentionPolicy.RetentionPeriod); ok {
			attrs["retention_period_seconds"] = v
		}
		attrs["retention_policy_locked"] = data.RetentionPolicy.IsLocked
	}
	return attrs
}

// storageBucketKMSKeyName returns the trimmed CMEK CryptoKey name from the
// bucket's default encryption configuration, or "" when the bucket uses
// Google-managed encryption (no encryption block).
func storageBucketKMSKeyName(data storageBucketData) string {
	if data.Encryption == nil {
		return ""
	}
	return strings.TrimSpace(data.Encryption.DefaultKMSKeyName)
}

// storageBucketLoggingDestination returns the trimmed destination log-bucket
// name from the bucket's usage-logging configuration, or "" when usage logging
// is disabled. The logObjectPrefix field is a data-plane object-naming detail
// and is never decoded.
func storageBucketLoggingDestination(data storageBucketData) string {
	if data.Logging == nil {
		return ""
	}
	return strings.TrimSpace(data.Logging.LogBucket)
}

// storageBucketEdge builds a supported typed relationship observation rooted at
// the bucket.
func storageBucketEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
