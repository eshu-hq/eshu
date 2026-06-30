// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const bigQueryTableFullName = "//bigquery.googleapis.com/projects/demo-project/datasets/analytics/tables/events"

func bigQueryTableContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigQueryTableFullName,
		AssetType:        assetTypeBigQueryTable,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigQueryTableExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigQueryTable); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigQueryTable)
	}
}

func TestExtractBigQueryTableManagedTable(t *testing.T) {
	const data = `{
		"tableReference": {"projectId": "demo-project", "datasetId": "analytics", "tableId": "events"},
		"type": "TABLE",
		"schema": {"fields": [
			{"name": "id", "type": "STRING"},
			{"name": "ts", "type": "TIMESTAMP"},
			{"name": "region", "type": "STRING"}
		]},
		"timePartitioning": {"type": "DAY", "field": "ts"},
		"clustering": {"fields": ["region", "id"]},
		"encryptionConfiguration": {"kmsKeyName": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1"},
		"numRows": "1000",
		"numBytes": "204800",
		"expirationTime": "1788220800000",
		"creationTime": "1717200000000",
		"location": "US"
	}`

	got, err := extractBigQueryTable(bigQueryTableContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"table_type":              "TABLE",
		"schema_field_count":      3,
		"time_partitioning_type":  "DAY",
		"time_partitioning_field": "ts",
		"clustering_fields":       []string{"region", "id"},
		"kms_key_name":            "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"num_rows":                int64(1000),
		"num_bytes":               int64(204800),
		"expiration_time":         "2026-09-01T00:00:00Z",
		"creation_time":           "2024-06-01T00:00:00Z",
		"location":                "US",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// Dataset + KMS anchors are cross-source join keys (full resource names).
	wantAnchors := []string{
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeBigQueryTableInDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics", assetTypeBigQueryDataset)
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryTableKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1", assetTypeKMSCryptoKey)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 relationships (dataset, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != bigQueryTableFullName {
			t.Errorf("relationship source = %q, want table full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeBigQueryTable {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeBigQueryTable)
		}
	}
}

func TestExtractBigQueryTableExternalSources(t *testing.T) {
	const data = `{
		"tableReference": {"projectId": "demo-project", "datasetId": "analytics", "tableId": "ext"},
		"type": "EXTERNAL",
		"externalDataConfiguration": {
			"sourceFormat": "CSV",
			"sourceUris": ["gs://landing-bucket/data/*.csv", "gs://landing-bucket/more.csv", "gs://archive-bucket/x.csv"]
		}
	}`

	got, err := extractBigQueryTable(bigQueryTableContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["table_type"] != "EXTERNAL" {
		t.Errorf("table_type = %v, want EXTERNAL", got.Attributes["table_type"])
	}
	if got.Attributes["external_source_format"] != "CSV" {
		t.Errorf("external_source_format = %v, want CSV", got.Attributes["external_source_format"])
	}
	// Two distinct buckets; object paths are dropped (data-plane locator).
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryTableExternalSource,
		"//storage.googleapis.com/projects/_/buckets/landing-bucket", assetTypeStorageBucket)
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryTableExternalSource,
		"//storage.googleapis.com/projects/_/buckets/archive-bucket", assetTypeStorageBucket)
	externalCount := 0
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeBigQueryTableExternalSource {
			externalCount++
		}
	}
	if externalCount != 2 {
		t.Fatalf("expected 2 deduped bucket edges, got %d: %#v", externalCount, got.Relationships)
	}
}

func TestExtractBigQueryTableNoLeakageOfObjectPaths(t *testing.T) {
	const data = `{
		"tableReference": {"projectId": "demo-project", "datasetId": "analytics", "tableId": "ext"},
		"type": "EXTERNAL",
		"externalDataConfiguration": {"sourceUris": ["gs://secret-bucket/private/customers.csv"]}
	}`
	got, err := extractBigQueryTable(bigQueryTableContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"private/customers.csv", "customers.csv", "gs://"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked object path token %q: %s", banned, blob)
		}
	}
}

func TestExtractBigQueryTableEmptyDataDerivesDatasetFromName(t *testing.T) {
	// Empty resource.data still carries the parent dataset because the table's own
	// full resource name encodes it; no attributes, KMS, or external edges appear.
	got, err := extractBigQueryTable(bigQueryTableContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the derived dataset edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryTableInDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics", assetTypeBigQueryDataset)
}

func TestExtractBigQueryTableMalformedDataErrors(t *testing.T) {
	_, err := extractBigQueryTable(bigQueryTableContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func assertRelationship(t *testing.T, rels []RelationshipObservation, relType, target, targetType string) {
	t.Helper()
	for _, rel := range rels {
		if rel.RelationshipType == relType && rel.TargetFullResourceName == target {
			if rel.TargetAssetType != targetType {
				t.Errorf("relationship %q target asset type = %q, want %q", relType, rel.TargetAssetType, targetType)
			}
			return
		}
	}
	t.Errorf("missing relationship type=%q target=%q in %#v", relType, target, rels)
}

func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
