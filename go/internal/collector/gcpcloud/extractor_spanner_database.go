// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"regexp"
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

// spannerDatabaseFullNamePattern anchors the exact CAI full-resource-name
// shape documented for a Spanner Database:
// //spanner.googleapis.com/projects/<p>/instances/<i>/databases/<d>. It is
// deliberately a full-string anchor (^...$), not a substring search: a loose
// Index("/databases/")+Contains("/instances/") check would also accept an
// extra segment between the instance and the databases marker
// (.../instances/<i>/extra/databases/<d>) or a trailing segment after the
// database id, silently deriving a wrong parent instance. Each id segment is
// `[^/]+` so a blank instance or database id (a dangling "//" or a trailing
// "/") never matches.
var spannerDatabaseFullNamePattern = regexp.MustCompile(
	`^//spanner\.googleapis\.com/projects/[^/]+/instances/([^/]+)/databases/[^/]+$`,
)

func init() {
	RegisterAssetExtractor(assetTypeSpannerDatabase, extractSpannerDatabase)
}

// spannerDatabaseData is the bounded view of a CAI spanner.googleapis.com/Database
// resource.data blob, verified field-for-field against the live Spanner v1
// discovery document's Database schema. EnableDropProtection is a *bool so a
// present `false` (an explicit posture) is distinguishable from a genuinely
// absent field — the same tri-state treatment the Backend Service extractor
// gives EnableCDN. EncryptionConfig carries both the singular KMSKeyName and
// the plural KMSKeyNames[]: the discovery document defines them as
// independent fields (KMSKeyNames covers a multi-region instance
// configuration that needs more than one regional key), not a
// deprecated/replacement pair, so both must be decoded and both resolve to
// edges. EncryptionInfo (Cloud KMS key-version usage detail, not a key
// resource name) and RestoreInfo (the backup/source-database reference for a
// restored database) are intentionally not declared as struct fields at all,
// so neither is ever decoded into Go memory.
type spannerDatabaseData struct {
	State                  string `json:"state"`
	CreateTime             string `json:"createTime"`
	VersionRetentionPeriod string `json:"versionRetentionPeriod"`
	EarliestVersionTime    string `json:"earliestVersionTime"`
	DefaultLeader          string `json:"defaultLeader"`
	DatabaseDialect        string `json:"databaseDialect"`
	EnableDropProtection   *bool  `json:"enableDropProtection"`
	EncryptionConfig       *struct {
		KMSKeyName  string   `json:"kmsKeyName"`
		KMSKeyNames []string `json:"kmsKeyNames"`
	} `json:"encryptionConfig"`
}

// extractSpannerDatabase extracts bounded, redaction-safe typed depth for one
// Cloud Spanner Database CAI asset. It returns the Terraform/drift/monitoring
// attribute set (lifecycle state, dialect, version-retention period,
// earliest-version time, create time, default leader region, and
// drop-protection posture); the parent Instance and every resolved CMEK
// CryptoKey full resource name as correlation anchors; and the typed
// spanner_database_in_instance (derived from the database's own resource
// name, since the Database resource carries no separate parent-instance
// field) and spanner_database_encrypted_by_kms_key edges — one per resolved
// key, since a multi-region instance configuration can require more than one
// regional key (encryptionConfig.kmsKeyNames[]) in addition to, or instead of,
// the singular encryptionConfig.kmsKeyName. This is the database-side half of
// the ownership split #4317 deliberately deferred: the Instance extractor
// emits no CMEK edge because CMEK is a per-database property, the same way
// the BigQuery Table extractor owns the Table→Dataset edge rather than the
// Dataset enumerating its Tables.
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
		// kmsKeyName (singular) and kmsKeyNames[] (plural) are independent
		// fields per the discovery document, not a deprecated/replacement
		// pair: kmsKeyNames[] covers a multi-region instance configuration
		// that needs more than one regional key to fully cover its regions.
		// Both are collected into one candidate list so a duplicate between
		// them, or within kmsKeyNames[] itself, is deduplicated by
		// dedupeNonEmpty rather than emitting a repeat edge.
		candidates := make([]string, 0, 1+len(data.EncryptionConfig.KMSKeyNames))
		candidates = append(candidates, data.EncryptionConfig.KMSKeyName)
		candidates = append(candidates, data.EncryptionConfig.KMSKeyNames...)
		for _, kmsName := range dedupeNonEmpty(kmsFullResourceNames(candidates)) {
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

// kmsFullResourceNames normalizes each candidate CMEK key reference through
// the shared cmekKeyFullResourceName helper, dropping any that fail closed
// (blank or wrong-domain). Order is preserved; dedupe is the caller's
// responsibility via dedupeNonEmpty.
func kmsFullResourceNames(candidates []string) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if kmsName := cmekKeyFullResourceName(candidate); kmsName != "" {
			out = append(out, kmsName)
		}
	}
	return out
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
// (//spanner.googleapis.com/projects/<p>/instances/<i>/databases/<d>). It
// matches the full string against the anchored spannerDatabaseFullNamePattern
// rather than a loose substring search: an unanchored
// Index("/databases/")+Contains("/instances/") check would also accept an
// extra segment between the instance and the databases marker
// (.../instances/<i>/extra/databases/<d>), silently deriving a wrong parent
// (".../instances/<i>/extra"), or a trailing segment after the database id.
// A name that does not match the exact documented shape fails closed (returns
// ""), so a malformed or unexpected full resource name never mints a
// fabricated parent edge.
func spannerDatabaseParentInstanceFullName(fullResourceName string) string {
	trimmed := strings.TrimSpace(fullResourceName)
	loc := spannerDatabaseFullNamePattern.FindStringSubmatchIndex(trimmed)
	if loc == nil {
		return ""
	}
	// loc[3] is the end offset of capture group 1 (the instance id), so
	// trimmed[:loc[3]] is exactly "//spanner.googleapis.com/projects/<p>/instances/<i>".
	return trimmed[:loc[3]]
}
