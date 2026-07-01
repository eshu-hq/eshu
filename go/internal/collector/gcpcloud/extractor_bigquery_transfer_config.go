// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeBigQueryTransferConfig is the CAI asset type for a BigQuery Data
// Transfer Service config. Its edge targets reuse asset-type constants and
// helpers already declared in this package: assetTypeBigQueryDataset and
// bigQueryResourceNamePrefix (BigQuery Table extractor), assetTypeKMSCryptoKey
// and cloudKMSResourceNamePrefix (BigQuery Table extractor), and
// assetTypePubSubTopic with pubSubTopicRefFullName (Pub/Sub extractors).
const assetTypeBigQueryTransferConfig = "bigquerydatatransfer.googleapis.com/TransferConfig"

// Bounded provider relationship types for BigQuery Transfer Config edges. The
// data source is an enumerated source id (not a CAI resource) kept as an
// attribute, and the runtime service account is carried as a fingerprint anchor
// (an email is not an exactly resolvable CAI endpoint), so neither becomes an
// edge.
const (
	relationshipTypeTransferConfigWritesToDataset   = "transfer_config_writes_to_dataset"
	relationshipTypeTransferConfigEncryptedByKMSKey = "transfer_config_encrypted_by_kms_key"
	relationshipTypeTransferConfigNotifiesTopic     = "transfer_config_notifies_topic"
)

func init() {
	RegisterAssetExtractor(assetTypeBigQueryTransferConfig, extractBigQueryTransferConfig)
}

// bigQueryTransferConfigData is the bounded view of a CAI
// bigquerydatatransfer.googleapis.com/TransferConfig resource.data blob. The
// params map (user-authored query text, source object paths, and other
// data-source-specific values) is intentionally not decoded and never persisted.
type bigQueryTransferConfigData struct {
	DataSourceID         string `json:"dataSourceId"`
	DestinationDatasetID string `json:"destinationDatasetId"`
	Schedule             string `json:"schedule"`
	State                string `json:"state"`
	Disabled             *bool  `json:"disabled"`
	ServiceAccountName   string `json:"serviceAccountName"`
	NotificationTopic    string `json:"notificationPubsubTopic"`
	EncryptionConfig     *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfiguration"`
}

// extractBigQueryTransferConfig extracts bounded, redaction-safe typed depth for
// one BigQuery Data Transfer Config CAI asset. It returns the
// Terraform/drift/monitoring attribute set (data source id, schedule, lifecycle
// state, disabled posture, CMEK posture, and fingerprinted runtime
// service-account email) and the typed destination-dataset, CMEK, and
// notification-topic edges. The transfer params are never read.
func extractBigQueryTransferConfig(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigQueryTransferConfigData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigquery transfer config data: %w", err)
	}

	attrs := bigQueryTransferConfigAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if dataset := bigQueryTransferConfigDatasetFullName(ctx, data); dataset != "" {
		anchors = append(anchors, dataset)
		rels = append(rels, bigQueryTransferConfigEdge(ctx, relationshipTypeTransferConfigWritesToDataset, dataset, assetTypeBigQueryDataset))
	}
	if fp := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccountName); fp != "" {
		anchors = append(anchors, fp)
	}
	if topic := pubSubTopicRefFullName(data.NotificationTopic); topic != "" {
		anchors = append(anchors, topic)
		rels = append(rels, bigQueryTransferConfigEdge(ctx, relationshipTypeTransferConfigNotifiesTopic, topic, assetTypePubSubTopic))
	}
	if data.EncryptionConfig != nil {
		if kms := bigQueryTransferConfigKMSKeyFullName(data.EncryptionConfig.KMSKeyName); kms != "" {
			anchors = append(anchors, kms)
			rels = append(rels, bigQueryTransferConfigEdge(ctx, relationshipTypeTransferConfigEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// bigQueryTransferConfigAttributes assembles the bounded attribute map. Empty,
// absent, or default-valued fields are omitted rather than written as zero
// values so a partial CAI page does not fabricate a posture.
func bigQueryTransferConfigAttributes(data bigQueryTransferConfigData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.DataSourceID); v != "" {
		attrs["data_source_id"] = v
	}
	if v := strings.TrimSpace(data.Schedule); v != "" {
		attrs["schedule"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}
	if data.EncryptionConfig != nil && strings.TrimSpace(data.EncryptionConfig.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if fp := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccountName); fp != "" {
		attrs["service_account_fingerprint"] = fp
	}
	return attrs
}

// bigQueryTransferConfigDatasetFullName builds the destination dataset CAI full
// resource name from destinationDatasetId, resolved against the transfer
// config's project. It returns "" when no destination dataset is set.
func bigQueryTransferConfigDatasetFullName(ctx ExtractContext, data bigQueryTransferConfigData) string {
	dataset := strings.TrimSpace(data.DestinationDatasetID)
	project := strings.TrimSpace(ctx.ProjectID)
	if dataset == "" || project == "" {
		return ""
	}
	return fmt.Sprintf("%sprojects/%s/datasets/%s", bigQueryResourceNamePrefix, project, dataset)
}

// bigQueryTransferConfigKMSKeyFullName builds the CAI CryptoKey full resource
// name from a relative KMS key name. An already-normalized CAI full resource
// name is returned unchanged. It returns "" for a blank reference.
func bigQueryTransferConfigKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// bigQueryTransferConfigEdge builds one typed provider relationship observation
// anchored on the transfer config's CAI full resource name.
func bigQueryTransferConfigEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
