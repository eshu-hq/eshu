// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestBigQueryDatasetOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for BigQuery Dataset through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the typed-depth attributes, correlation
// anchors, and the KMS edge reach the durable facts without any live GCP call.
func TestBigQueryDatasetOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_bigquery_dataset.json"))
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
	relTypes := map[string]int{}
	var analyticsAttrs map[string]any
	var analyticsAnchors []any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == "//bigquery.googleapis.com/projects/demo-project/datasets/analytics" {
				analyticsAttrs, _ = env.Payload["attributes"].(map[string]any)
				analyticsAnchors, _ = env.Payload["correlation_anchors"].([]any)
				if analyticsAnchors == nil {
					if s, ok := env.Payload["correlation_anchors"].([]string); ok {
						for _, v := range s {
							analyticsAnchors = append(analyticsAnchors, v)
						}
					}
				}
			}
		case facts.GCPCloudRelationshipFactKind:
			relTypes[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if analyticsAttrs == nil {
		t.Fatalf("analytics dataset carried no attributes")
	}
	if analyticsAttrs["location"] != "US" {
		t.Errorf("analytics location = %v, want US", analyticsAttrs["location"])
	}
	if analyticsAttrs["kms_key_name"] != "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1" {
		t.Errorf("analytics kms_key_name = %v", analyticsAttrs["kms_key_name"])
	}
	if analyticsAttrs["access_entry_count"] != float64(3) && analyticsAttrs["access_entry_count"] != 3 {
		t.Errorf("analytics access_entry_count = %v, want 3", analyticsAttrs["access_entry_count"])
	}
	if len(analyticsAnchors) == 0 {
		t.Errorf("analytics dataset carried no correlation anchors")
	}

	// Only the KMS-encrypted dataset (analytics) emits an edge; raw has no KMS.
	if relTypes[relationshipTypeBigQueryDatasetKMSKey] != 1 {
		t.Errorf("kms edges = %d, want 1", relTypes[relationshipTypeBigQueryDatasetKMSKey])
	}
}
