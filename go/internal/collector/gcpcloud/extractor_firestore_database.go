// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeFirestoreDatabase is the CAI asset type for a Firestore database. Its
// CMEK edge target reuses assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix
// already declared elsewhere in this package.
const assetTypeFirestoreDatabase = "firestore.googleapis.com/Database"

// relationshipTypeFirestoreEncryptedByKMSKey is the bounded provider
// relationship type carried on the Firestore CMEK edge; the reducer materializes
// it only when both endpoints resolve exactly.
const relationshipTypeFirestoreEncryptedByKMSKey = "firestore_database_encrypted_by_kms_key"

func init() {
	RegisterAssetExtractor(assetTypeFirestoreDatabase, extractFirestoreDatabase)
}

// firestoreDatabaseData is the bounded view of a CAI
// firestore.googleapis.com/Database resource.data blob. Only redaction-safe
// control-plane metadata and the CMEK key reference are decoded; a Firestore
// database asset carries no document data to omit.
type firestoreDatabaseData struct {
	Type                          string `json:"type"`
	LocationID                    string `json:"locationId"`
	ConcurrencyMode               string `json:"concurrencyMode"`
	AppEngineIntegrationMode      string `json:"appEngineIntegrationMode"`
	PointInTimeRecoveryEnablement string `json:"pointInTimeRecoveryEnablement"`
	DeleteProtectionState         string `json:"deleteProtectionState"`
	CreateTime                    string `json:"createTime"`
	CMEKConfig                    *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"cmekConfig"`
}

// extractFirestoreDatabase extracts bounded, redaction-safe typed depth for one
// Firestore Database CAI asset. It returns the Terraform/drift/monitoring
// attribute set (database mode, location, concurrency mode, App Engine
// integration mode, point-in-time-recovery and delete-protection posture,
// customer-managed-encryption posture, and creation time), the CMEK CryptoKey
// resource name as a correlation anchor, and the typed
// firestore_database_encrypted_by_kms_key edge.
func extractFirestoreDatabase(ctx ExtractContext) (AttributeExtraction, error) {
	var data firestoreDatabaseData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode firestore database data: %w", err)
	}

	attrs := firestoreDatabaseAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if data.CMEKConfig != nil {
		if kmsName := firestoreKMSKeyFullName(data.CMEKConfig.KMSKeyName); kmsName != "" {
			anchors = append(anchors, kmsName)
			rels = append(rels, firestoreDatabaseEdge(ctx, relationshipTypeFirestoreEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// firestoreDatabaseAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial CAI
// page does not fabricate a posture.
func firestoreDatabaseAttributes(data firestoreDatabaseData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["database_type"] = v
	}
	if v := strings.TrimSpace(data.LocationID); v != "" {
		attrs["location_id"] = v
	}
	if v := strings.TrimSpace(data.ConcurrencyMode); v != "" {
		attrs["concurrency_mode"] = v
	}
	if v := strings.TrimSpace(data.AppEngineIntegrationMode); v != "" {
		attrs["app_engine_integration_mode"] = v
	}
	if v := strings.TrimSpace(data.PointInTimeRecoveryEnablement); v != "" {
		attrs["point_in_time_recovery"] = v
	}
	if v := strings.TrimSpace(data.DeleteProtectionState); v != "" {
		attrs["delete_protection"] = v
	}
	if data.CMEKConfig != nil && strings.TrimSpace(data.CMEKConfig.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// firestoreKMSKeyFullName builds the CAI CryptoKey full resource name from a
// relative KMS key name. An already-normalized CAI full resource name is
// returned unchanged. It returns "" for a blank reference so the caller emits no
// encryption edge.
func firestoreKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// firestoreDatabaseEdge builds one typed provider relationship observation
// anchored on the Firestore database's CAI full resource name.
func firestoreDatabaseEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
