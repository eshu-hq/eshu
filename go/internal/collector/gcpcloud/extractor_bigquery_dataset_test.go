// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const bigQueryDatasetFullResourceName = "//bigquery.googleapis.com/projects/demo-project/datasets/analytics"

func bigQueryDatasetContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigQueryDatasetFullResourceName,
		AssetType:        assetTypeBigQueryDataset,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigQueryDatasetExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigQueryDataset); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigQueryDataset)
	}
}

func TestExtractBigQueryDatasetFullAttributes(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "analytics"},
		"friendlyName": "Analytics",
		"location": "US",
		"defaultTableExpirationMs": "3600000",
		"defaultPartitionExpirationMs": "7200000",
		"creationTime": "1717200000000",
		"lastModifiedTime": "1788220800000",
		"defaultEncryptionConfiguration": {"kmsKeyName": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1"},
		"access": [
			{"role": "OWNER", "userByEmail": "alice@example.com"},
			{"role": "WRITER", "specialGroup": "projectWriters"},
			{"role": "READER", "groupByEmail": "team@example.com"},
			{"role": "roles/bigquery.dataViewer", "iamMember": "serviceAccount:svc@demo-project.iam.gserviceaccount.com"}
		]
	}`

	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location":                        "US",
		"default_table_expiration_ms":     int64(3600000),
		"default_partition_expiration_ms": int64(7200000),
		"creation_time":                   "2024-06-01T00:00:00Z",
		"last_modified_time":              "2026-09-01T00:00:00Z",
		"kms_key_name":                    "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"access_entry_count":              4,
		"access_roles":                    []string{"OWNER", "READER", "WRITER", "roles/bigquery.dataViewer"},
		"access_member_classes":           []string{"group", "serviceAccount", "special", "user"},
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// Only the KMS key resolves to a CAI asset endpoint.
	wantAnchors := []string{
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeBigQueryDatasetKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1", assetTypeKMSCryptoKey)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the KMS edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != bigQueryDatasetFullResourceName {
		t.Errorf("relationship source = %q, want dataset full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != assetTypeBigQueryDataset {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeBigQueryDataset)
	}
}

func TestExtractBigQueryDatasetNoEncryptionNoAccess(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "raw"},
		"location": "EU"
	}`
	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["location"] != "EU" {
		t.Errorf("location = %v, want EU", got.Attributes["location"])
	}
	if _, ok := got.Attributes["access_entry_count"]; ok {
		t.Errorf("expected no access_entry_count for empty access, got %#v", got.Attributes["access_entry_count"])
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges without KMS, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors without KMS, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractBigQueryDatasetNoMemberLeakage(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "analytics"},
		"access": [
			{"role": "OWNER", "userByEmail": "secret.person@example.com"},
			{"role": "READER", "groupByEmail": "confidential-team@example.com"},
			{"role": "READER", "iamMember": "serviceAccount:leaky-svc@demo-project.iam.gserviceaccount.com"}
		]
	}`
	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{
		"secret.person@example.com", "confidential-team@example.com",
		"leaky-svc@demo-project.iam.gserviceaccount.com", "@example.com", "gserviceaccount.com",
	} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked IAM member token %q: %s", banned, blob)
		}
	}
	if got.Attributes["access_entry_count"] != 3 {
		t.Errorf("access_entry_count = %v, want 3", got.Attributes["access_entry_count"])
	}
}

func TestExtractBigQueryDatasetAuthorizedResources(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "analytics"},
		"access": [
			{"view": {"projectId": "shared-project", "datasetId": "curated", "tableId": "v_events"}},
			{"routine": {"projectId": "demo-project", "datasetId": "udfs", "routineId": "mask_pii"}},
			{"dataset": {"dataset": {"projectId": "demo-project", "datasetId": "raw"}, "targetTypes": ["VIEWS"]}},
			{"view": {"datasetId": "curated", "tableId": "v_events"}}
		]
	}`
	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeBigQueryDatasetAuthorizesView,
		"//bigquery.googleapis.com/projects/shared-project/datasets/curated/tables/v_events", assetTypeBigQueryTable)
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryDatasetAuthorizesRoutine,
		"//bigquery.googleapis.com/projects/demo-project/datasets/udfs/routines/mask_pii", assetTypeBigQueryRoutine)
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryDatasetAuthorizesDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/raw", assetTypeBigQueryDataset)
	// The fourth entry omits projectId; it falls back to the dataset's own
	// project (demo-project) and thus dedupes against neither the shared-project
	// view nor itself twice.
	assertRelationship(t, got.Relationships, relationshipTypeBigQueryDatasetAuthorizesView,
		"//bigquery.googleapis.com/projects/demo-project/datasets/curated/tables/v_events", assetTypeBigQueryTable)
	if len(got.Relationships) != 4 {
		t.Fatalf("expected 4 authorized-resource edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	// Every edge target is also a correlation anchor.
	if len(got.CorrelationAnchors) != 4 {
		t.Errorf("expected 4 anchors, got %d: %#v", len(got.CorrelationAnchors), got.CorrelationAnchors)
	}
	if got.Attributes["access_member_classes"] == nil {
		t.Fatalf("expected access_member_classes")
	}
	classes, _ := got.Attributes["access_member_classes"].([]string)
	if len(classes) != 1 || classes[0] != "view" {
		t.Errorf("access_member_classes = %#v, want [view]", classes)
	}
}

func TestExtractBigQueryDatasetAuthorizedViewDeduped(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "analytics"},
		"access": [
			{"view": {"projectId": "demo-project", "datasetId": "curated", "tableId": "v"}},
			{"view": {"projectId": "demo-project", "datasetId": "curated", "tableId": "v"}}
		]
	}`
	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 deduped view edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractBigQueryDatasetAllAuthenticatedUsersClass(t *testing.T) {
	const data = `{
		"datasetReference": {"projectId": "demo-project", "datasetId": "analytics"},
		"access": [
			{"role": "READER", "specialGroup": "allAuthenticatedUsers"},
			{"role": "OWNER", "specialGroup": "projectOwners"}
		]
	}`
	got, err := extractBigQueryDataset(bigQueryDatasetContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	classes, _ := got.Attributes["access_member_classes"].([]string)
	want := []string{"authenticated", "special"}
	if !reflect.DeepEqual(classes, want) {
		t.Errorf("access_member_classes = %#v, want %#v (allAuthenticatedUsers must surface as authenticated)", classes, want)
	}
}

func TestExtractBigQueryDatasetMalformedDataErrors(t *testing.T) {
	_, err := extractBigQueryDataset(bigQueryDatasetContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
