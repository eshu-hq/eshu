// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeSpannerDatabase is the Cloud Asset Inventory asset type for a Cloud
// Spanner Database. It is a separately CAI-inventoried asset type from
// spanner.googleapis.com/Instance (extractor_spanner_instance.go, #4317): the
// Instance resource carries no per-database detail or CMEK, both of which
// live on the Database resource, mirroring the BigQuery Table/Dataset
// ownership split.
const assetTypeSpannerDatabase = "spanner.googleapis.com/Database"

// Bounded relationship types for Spanner Database edges. They are stable
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeSpannerDatabaseInInstance        = "spanner_database_in_instance"
	relationshipTypeSpannerDatabaseEncryptedByKMSKey = "spanner_database_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeSpannerDatabase, extractSpannerDatabase)
}

// spannerDatabaseData is the bounded view of a CAI spanner.googleapis.com/Database
// resource.data blob, verified field-for-field against the live Spanner v1
// discovery document's Database schema. EnableDropProtection is a *bool so a
// present `false` (an explicit posture) is distinguishable from a genuinely
// absent field — the same tri-state treatment the Backend Service extractor
// gives EnableCDN. EncryptionInfo (Cloud KMS key-version usage detail) and
// RestoreInfo (the backup/source-database reference for a restored database)
// are intentionally not declared as struct fields at all, so neither is ever
// decoded into Go memory: encryptionConfig.kmsKeyName is the sole CMEK
// edge/anchor source of truth per the issue's contract, and a restore source
// carries no typed-depth value here.
type spannerDatabaseData struct {
	State                  string `json:"state"`
	CreateTime             string `json:"createTime"`
	VersionRetentionPeriod string `json:"versionRetentionPeriod"`
	EarliestVersionTime    string `json:"earliestVersionTime"`
	DefaultLeader          string `json:"defaultLeader"`
	DatabaseDialect        string `json:"databaseDialect"`
	EnableDropProtection   *bool  `json:"enableDropProtection"`
	EncryptionConfig       *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfig"`
}

// extractSpannerDatabase extracts bounded, redaction-safe typed depth for one
// Cloud Spanner Database CAI asset. It returns the Terraform/drift/monitoring
// attribute set (lifecycle state, dialect, version-retention period,
// earliest-version time, create time, default leader region, and
// drop-protection posture); the parent Instance and CMEK CryptoKey full
// resource names as correlation anchors; and the typed
// spanner_database_in_instance (derived from the database's own resource
// name, since the Database resource carries no separate parent-instance
// field) and spanner_database_encrypted_by_kms_key edges. This is the
// database-side half of the ownership split #4317 deliberately deferred: the
// Instance extractor emits no CMEK edge because CMEK is a per-database
// property, the same way the BigQuery Table extractor owns the Table→Dataset
// edge rather than the Dataset enumerating its Tables.
func extractSpannerDatabase(ctx ExtractContext) (AttributeExtraction, error) {
	var data spannerDatabaseData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode spanner database data: %w", err)
	}

	attrs := spannerDatabaseAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if instanceName := spannerDatabaseParentInstanceFullName(ctx.FullResourceName); instanceName != "" {
		anchors = append(anchors, instanceName)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeSpannerDatabaseInInstance,
			TargetFullResourceName: instanceName,
			TargetAssetType:        assetTypeSpannerInstance,
			SupportState:           RelationshipSupportSupported,
		})
	}

	if data.EncryptionConfig != nil {
		if kmsName := cmekKeyFullResourceName(data.EncryptionConfig.KMSKeyName); kmsName != "" {
			anchors = append(anchors, kmsName)
			rels = append(rels, RelationshipObservation{
				SourceFullResourceName: ctx.FullResourceName,
				SourceAssetType:        ctx.AssetType,
				RelationshipType:       relationshipTypeSpannerDatabaseEncryptedByKMSKey,
				TargetFullResourceName: kmsName,
				TargetAssetType:        assetTypeKMSCryptoKey,
				SupportState:           RelationshipSupportSupported,
			})
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// spannerDatabaseAttributes assembles the bounded attribute map. Blank string
// fields are omitted so a partial CAI page does not fabricate a posture.
// EnableDropProtection is the deliberate tri-state exception: an explicit
// false is kept since it is real posture (drop protection off), while only a
// genuinely absent field is omitted.
func spannerDatabaseAttributes(data spannerDatabaseData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.DatabaseDialect); v != "" {
		attrs["database_dialect"] = v
	}
	if v := strings.TrimSpace(data.VersionRetentionPeriod); v != "" {
		attrs["version_retention_period"] = v
	}
	if v := strings.TrimSpace(data.EarliestVersionTime); v != "" {
		attrs["earliest_version_time"] = v
	}
	if v := strings.TrimSpace(data.CreateTime); v != "" {
		attrs["create_time"] = v
	}
	if v := strings.TrimSpace(data.DefaultLeader); v != "" {
		attrs["default_leader"] = v
	}
	if data.EnableDropProtection != nil {
		attrs["enable_drop_protection"] = *data.EnableDropProtection
	}
	return attrs
}

// spannerDatabaseParentInstanceFullName derives the parent Spanner Instance
// full resource name from the database's own CAI full resource name. The
// Database resource.data blob carries no separate parent-instance field, so
// the parent is derived from the database's own identity path
// (//spanner.googleapis.com/projects/<p>/instances/<i>/databases/<d>),
// mirroring the Bigtable Cluster extractor's parent-Instance derivation. It
// fails closed (returns "") on any name that does not carry the exact
// documented "/instances/<id>/databases/<id>" shape, so a malformed or
// unexpected full resource name never mints a fabricated parent edge.
func spannerDatabaseParentInstanceFullName(fullResourceName string) string {
	trimmed := strings.TrimSpace(fullResourceName)
	idx := strings.Index(trimmed, "/databases/")
	if idx <= 0 {
		return ""
	}
	instancePart := trimmed[:idx]
	if !strings.Contains(instancePart, "/instances/") {
		return ""
	}
	return instancePart
}
