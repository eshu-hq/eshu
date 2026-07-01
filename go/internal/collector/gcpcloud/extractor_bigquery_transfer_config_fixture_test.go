// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestBigQueryTransferConfigOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for BigQuery Data Transfer Config through parse ->
// normalize -> attribute extraction -> generation -> envelope, proving the
// redaction-safe typed-depth attributes and the dataset / notification-topic /
// CMEK edges reach durable facts without any live GCP call, and that no transfer
// params or raw service-account email lands on a fact.
func TestBigQueryTransferConfigOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_bigquery_transfer_config.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
	}

	gen := NewGeneration(attributesTestBoundary(), redact.Key{})
	if err := gen.AddPage(page.Resources); err != nil {
		t.Fatalf("add page: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("build generation: %v", err)
	}

	resourceCount := 0
	edges := map[string]int{}
	var loadAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == bigQueryTransferConfigFullName {
				loadAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			edges[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if loadAttrs == nil {
		t.Fatalf("gcs-load transfer config carried no attributes")
	}
	if loadAttrs["data_source_id"] != "google_cloud_storage" {
		t.Errorf("data_source_id = %v, want google_cloud_storage", loadAttrs["data_source_id"])
	}
	for rel, want := range map[string]int{
		relationshipTypeTransferConfigWritesToDataset:   2,
		relationshipTypeTransferConfigNotifiesTopic:     1,
		relationshipTypeTransferConfigEncryptedByKMSKey: 1,
	} {
		if edges[rel] != want {
			t.Errorf("%s edges = %d, want %d", rel, edges[rel], want)
		}
	}

	// No transfer params or raw service-account email may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"placeholder-bucket", "SELECT 1", "data_path_template", "params", "transfer-runner@demo-project"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked token %q", token)
		}
	}
}
