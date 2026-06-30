// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Asset type constants for the BigQuery Table typed-depth extractor and the
// relationship endpoints it derives. Target asset types name the CAI asset type
// of each typed edge so reducers can resolve both endpoints exactly.
const (
	assetTypeBigQueryTable   = "bigquery.googleapis.com/Table"
	assetTypeBigQueryDataset = "bigquery.googleapis.com/Dataset"
	assetTypeKMSCryptoKey    = "cloudkms.googleapis.com/CryptoKey"
	assetTypeStorageBucket   = "storage.googleapis.com/Bucket"
)

// Bounded relationship types for BigQuery Table edges. They are stable, bounded
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeBigQueryTableInDataset      = "bigquery_table_in_dataset"
	relationshipTypeBigQueryTableKMSKey         = "bigquery_table_encrypted_by_kms_key"
	relationshipTypeBigQueryTableExternalSource = "bigquery_table_reads_external_source"
	cloudKMSResourceNamePrefix                  = "//cloudkms.googleapis.com/"
	storageBucketResourceNamePrefixFmt          = "//storage.googleapis.com/projects/_/buckets/"
	bigQueryResourceNamePrefix                  = "//bigquery.googleapis.com/"
)

func init() {
	RegisterAssetExtractor(assetTypeBigQueryTable, extractBigQueryTable)
}

// bigQueryTableData is the bounded view of a CAI bigquery.googleapis.com/Table
// resource.data blob. Only fields that are safe control-plane metadata or
// resource identifiers are decoded; the rest of the blob (and any data-plane
// content) is never read. numRows/numBytes/expirationTime/creationTime arrive as
// JSON strings in the BigQuery REST representation.
type bigQueryTableData struct {
	TableReference struct {
		ProjectID string `json:"projectId"`
		DatasetID string `json:"datasetId"`
		TableID   string `json:"tableId"`
	} `json:"tableReference"`
	Type   string `json:"type"`
	Schema struct {
		Fields []json.RawMessage `json:"fields"`
	} `json:"schema"`
	TimePartitioning *struct {
		Type  string `json:"type"`
		Field string `json:"field"`
	} `json:"timePartitioning"`
	Clustering *struct {
		Fields []string `json:"fields"`
	} `json:"clustering"`
	EncryptionConfiguration *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfiguration"`
	NumRows                   string `json:"numRows"`
	NumBytes                  string `json:"numBytes"`
	ExpirationTime            string `json:"expirationTime"`
	CreationTime              string `json:"creationTime"`
	Location                  string `json:"location"`
	ExternalDataConfiguration *struct {
		SourceFormat string   `json:"sourceFormat"`
		SourceURIs   []string `json:"sourceUris"`
	} `json:"externalDataConfiguration"`
}

// extractBigQueryTable extracts bounded typed depth for one BigQuery Table CAI
// asset. It returns the Terraform/drift/monitoring attribute set, cross-source
// correlation anchors (dataset and KMS key resource names), and typed parent
// Dataset, KMS encryption, and external GCS source relationships. Object paths
// inside external source URIs are data-plane locators and are dropped: only the
// bucket identity is kept as an edge endpoint.
func extractBigQueryTable(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigQueryTableData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigquery table data: %w", err)
	}

	attrs := bigQueryTableAttributes(data)
	anchors := make([]string, 0, 2)
	rels := make([]RelationshipObservation, 0, 4)

	datasetName := bigQueryDatasetFullName(ctx, data)
	if datasetName != "" {
		anchors = append(anchors, datasetName)
		rels = append(rels, bigQueryTableEdge(ctx, relationshipTypeBigQueryTableInDataset, datasetName, assetTypeBigQueryDataset))
	}

	if kms := encryptionKMSKeyName(data); kms != "" {
		attrs["kms_key_name"] = kms
		kmsName := cloudKMSResourceNamePrefix + kms
		anchors = append(anchors, kmsName)
		rels = append(rels, bigQueryTableEdge(ctx, relationshipTypeBigQueryTableKMSKey, kmsName, assetTypeKMSCryptoKey))
	}

	for _, bucketName := range externalBucketNames(data) {
		rels = append(rels, bigQueryTableEdge(ctx, relationshipTypeBigQueryTableExternalSource, bucketName, assetTypeStorageBucket))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// bigQueryTableAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate "0 rows" or an empty partitioning scheme.
func bigQueryTableAttributes(data bigQueryTableData) map[string]any {
	attrs := map[string]any{}
	if t := strings.TrimSpace(data.Type); t != "" {
		attrs["table_type"] = t
	}
	if n := len(data.Schema.Fields); n > 0 {
		attrs["schema_field_count"] = n
	}
	if data.TimePartitioning != nil {
		if v := strings.TrimSpace(data.TimePartitioning.Type); v != "" {
			attrs["time_partitioning_type"] = v
		}
		if v := strings.TrimSpace(data.TimePartitioning.Field); v != "" {
			attrs["time_partitioning_field"] = v
		}
	}
	if data.Clustering != nil {
		if fields := trimmedStrings(data.Clustering.Fields); len(fields) > 0 {
			attrs["clustering_fields"] = fields
		}
	}
	if v, ok := parseInt64String(data.NumRows); ok {
		attrs["num_rows"] = v
	}
	if v, ok := parseInt64String(data.NumBytes); ok {
		attrs["num_bytes"] = v
	}
	if v, ok := epochMillisToRFC3339(data.ExpirationTime); ok {
		attrs["expiration_time"] = v
	}
	if v, ok := epochMillisToRFC3339(data.CreationTime); ok {
		attrs["creation_time"] = v
	}
	if v := strings.TrimSpace(data.Location); v != "" {
		attrs["location"] = v
	}
	if data.ExternalDataConfiguration != nil {
		if v := strings.TrimSpace(data.ExternalDataConfiguration.SourceFormat); v != "" {
			attrs["external_source_format"] = v
		}
	}
	return attrs
}

// bigQueryDatasetFullName derives the parent dataset full resource name from the
// table reference, falling back to trimming the table segment from the table's
// own full resource name when the reference is incomplete.
func bigQueryDatasetFullName(ctx ExtractContext, data bigQueryTableData) string {
	project := firstNonEmpty(data.TableReference.ProjectID, ctx.ProjectID)
	dataset := strings.TrimSpace(data.TableReference.DatasetID)
	if project != "" && dataset != "" {
		return fmt.Sprintf("%sprojects/%s/datasets/%s", bigQueryResourceNamePrefix, project, dataset)
	}
	if idx := strings.Index(ctx.FullResourceName, "/tables/"); idx > 0 {
		return ctx.FullResourceName[:idx]
	}
	return ""
}

func encryptionKMSKeyName(data bigQueryTableData) string {
	if data.EncryptionConfiguration == nil {
		return ""
	}
	return strings.TrimSpace(data.EncryptionConfiguration.KMSKeyName)
}

// externalBucketNames derives deduplicated GCS bucket full resource names from
// external source URIs. The object path after the bucket is a data-plane locator
// and is intentionally discarded; only the bucket identity becomes an edge.
func externalBucketNames(data bigQueryTableData) []string {
	if data.ExternalDataConfiguration == nil {
		return nil
	}
	names := make([]string, 0, len(data.ExternalDataConfiguration.SourceURIs))
	for _, uri := range data.ExternalDataConfiguration.SourceURIs {
		bucket := gcsBucketFromURI(uri)
		if bucket == "" {
			continue
		}
		names = append(names, storageBucketResourceNamePrefixFmt+bucket)
	}
	return dedupeNonEmpty(names)
}

// gcsBucketFromURI extracts only the bucket name from a gs://bucket/object URI.
// The object path is never returned so no data-plane locator leaves the parser.
func gcsBucketFromURI(uri string) string {
	trimmed := strings.TrimSpace(uri)
	rest, ok := strings.CutPrefix(trimmed, "gs://")
	if !ok {
		return ""
	}
	bucket, _, _ := strings.Cut(rest, "/")
	return strings.TrimSpace(bucket)
}

func bigQueryTableEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}

func parseInt64String(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// epochMillisToRFC3339 converts a BigQuery epoch-millisecond string into an
// RFC3339 UTC timestamp. A blank or unparseable value yields ok=false so the
// attribute is omitted rather than written as the zero time.
func epochMillisToRFC3339(value string) (string, bool) {
	millis, ok := parseInt64String(value)
	if !ok {
		return "", false
	}
	return time.UnixMilli(millis).UTC().Format(time.RFC3339), true
}

func trimmedStrings(input []string) []string {
	out := make([]string, 0, len(input))
	for _, v := range input {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dedupeNonEmpty(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, v := range input {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
