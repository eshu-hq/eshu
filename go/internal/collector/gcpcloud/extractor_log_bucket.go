// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// logBucketAssetType is the Cloud Asset Inventory asset type for a GCP Logging
// Log Bucket. The CMEK CryptoKey endpoint reuses assetTypeKMSCryptoKey and
// cloudKMSResourceNamePrefix already declared elsewhere in this package.
const logBucketAssetType = "logging.googleapis.com/LogBucket"

// relationshipTypeLogBucketEncryptedByKMSKey is the bounded provider relationship
// type for the edge from a log bucket to its CMEK CryptoKey.
const relationshipTypeLogBucketEncryptedByKMSKey = "log_bucket_encrypted_by_kms_key"

func init() {
	RegisterAssetExtractor(logBucketAssetType, extractLogBucket)
}

// logBucketData is the bounded view of a CAI logging.googleapis.com/LogBucket
// resource.data blob. Only redaction-safe control-plane posture is decoded; the
// only cross-source reference pulled is the CMEK CryptoKey resource name (not key
// material). Locked and AnalyticsEnabled are pointers so a present `false` is
// distinguishable from an absent field.
type logBucketData struct {
	RetentionDays    int    `json:"retentionDays"`
	Locked           *bool  `json:"locked"`
	AnalyticsEnabled *bool  `json:"analyticsEnabled"`
	CreateTime       string `json:"createTime"`
	CMEKSettings     *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"cmekSettings"`
}

// extractLogBucket extracts bounded, redaction-safe typed depth for one CAI
// Logging Log Bucket asset. It surfaces the retention period, locked and
// analytics posture, creation time, and CMEK posture; and emits the typed
// log_bucket_encrypted_by_kms_key edge to the CMEK CryptoKey with the key
// resource name as the correlation anchor.
//
// The bucket's linked analytics BigQuery datasets are separate Link
// sub-resources (not on the bucket's own data) and its owning project is
// base-observation ancestry, so the only outbound edge is the CMEK key.
func extractLogBucket(ctx ExtractContext) (AttributeExtraction, error) {
	var data logBucketData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode log bucket data: %w", err)
	}

	attrs := map[string]any{}
	if data.RetentionDays > 0 {
		attrs["retention_days"] = data.RetentionDays
	}
	if data.Locked != nil {
		attrs["locked"] = *data.Locked
	}
	if data.AnalyticsEnabled != nil {
		attrs["analytics_enabled"] = *data.AnalyticsEnabled
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}

	var anchors []string
	var rels []RelationshipObservation
	if data.CMEKSettings != nil {
		if keyName := logBucketKMSKeyFullName(data.CMEKSettings.KMSKeyName); keyName != "" {
			attrs["customer_managed_encryption"] = true
			anchors = append(anchors, keyName)
			rels = append(rels, RelationshipObservation{
				SourceFullResourceName: ctx.FullResourceName,
				SourceAssetType:        ctx.AssetType,
				RelationshipType:       relationshipTypeLogBucketEncryptedByKMSKey,
				TargetFullResourceName: keyName,
				TargetAssetType:        assetTypeKMSCryptoKey,
				SupportState:           RelationshipSupportSupported,
			})
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// logBucketKMSKeyFullName builds the CAI CryptoKey full resource name from a
// relative CMEK key name. An already-normalized CAI full resource name is
// returned unchanged so the prefix is never doubled; a blank reference yields "".
func logBucketKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}
