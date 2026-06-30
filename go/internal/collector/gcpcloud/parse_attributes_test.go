// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "testing"

// bigQueryTableAssetsListPage is one assets.list page carrying a single BigQuery
// Table asset with a KMS-encrypted, partitioned, clustered managed table.
const bigQueryTableAssetsListPage = `{
  "readTime": "2026-06-25T00:00:00Z",
  "assets": [
    {
      "name": "//bigquery.googleapis.com/projects/demo-project/datasets/analytics/tables/events",
      "assetType": "bigquery.googleapis.com/Table",
      "updateTime": "2026-06-24T12:00:00Z",
      "ancestors": ["projects/123456789", "organizations/987654321"],
      "resource": {
        "location": "US",
        "data": {
          "tableReference": {"projectId": "demo-project", "datasetId": "analytics", "tableId": "events"},
          "type": "TABLE",
          "schema": {"fields": [{"name": "id", "type": "STRING"}, {"name": "ts", "type": "TIMESTAMP"}]},
          "timePartitioning": {"type": "DAY", "field": "ts"},
          "encryptionConfiguration": {"kmsKeyName": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1"},
          "numRows": "42",
          "labels": {"team": "data"}
        }
      }
    }
  ]
}`

func TestParseAssetsListPageExtractsBigQueryTableTypedDepth(t *testing.T) {
	page, err := ParseAssetsListPage([]byte(bigQueryTableAssetsListPage))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(page.Resources))
	}
	obs := page.Resources[0]

	// Base bounded fields still parse.
	if obs.AssetType != assetTypeBigQueryTable {
		t.Errorf("asset type = %q", obs.AssetType)
	}
	if obs.Labels["team"] != "data" {
		t.Errorf("labels not parsed: %#v", obs.Labels)
	}

	// Typed depth attached via the registry.
	if got := obs.Attributes["table_type"]; got != "TABLE" {
		t.Errorf("attributes.table_type = %v, want TABLE", got)
	}
	if got := obs.Attributes["num_rows"]; got != int64(42) {
		t.Errorf("attributes.num_rows = %v (%T), want int64(42)", got, got)
	}
	if got := obs.Attributes["kms_key_name"]; got != "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1" {
		t.Errorf("attributes.kms_key_name = %v", got)
	}
	wantAnchors := map[string]bool{
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics":                       true,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1": true,
	}
	for _, anchor := range obs.CorrelationAnchors {
		delete(wantAnchors, anchor)
	}
	if len(wantAnchors) != 0 {
		t.Errorf("missing correlation anchors: %#v (got %#v)", wantAnchors, obs.CorrelationAnchors)
	}
	if len(obs.Relationships) != 2 {
		t.Fatalf("expected 2 typed relationships (dataset, kms), got %#v", obs.Relationships)
	}
}

func TestParseAssetsListPageLeavesUntypedAssetsWithoutAttributes(t *testing.T) {
	const page = `{
      "assets": [
        {
          "name": "//compute.googleapis.com/projects/demo/zones/us-central1-a/instances/vm-1",
          "assetType": "compute.googleapis.com/Instance",
          "resource": {"location": "us-central1-a", "data": {"displayName": "vm-1"}}
        }
      ]
    }`
	parsed, err := ParseAssetsListPage([]byte(page))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	obs := parsed.Resources[0]
	if obs.Attributes != nil {
		t.Errorf("expected nil attributes for an asset type with no extractor, got %#v", obs.Attributes)
	}
	if obs.CorrelationAnchors != nil {
		t.Errorf("expected nil anchors for an asset type with no extractor, got %#v", obs.CorrelationAnchors)
	}
}
