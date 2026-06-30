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

// TestBigQueryTableOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for BigQuery Table through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the typed-depth attributes, correlation
// anchors, and typed edges reach the durable facts without any live GCP call.
func TestBigQueryTableOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_bigquery_table.json"))
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
	var eventsAttrs map[string]any
	var eventsAnchors []any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == "//bigquery.googleapis.com/projects/demo-project/datasets/analytics/tables/events" {
				eventsAttrs, _ = env.Payload["attributes"].(map[string]any)
				eventsAnchors, _ = env.Payload["correlation_anchors"].([]any)
				if eventsAnchors == nil {
					if s, ok := env.Payload["correlation_anchors"].([]string); ok {
						for _, v := range s {
							eventsAnchors = append(eventsAnchors, v)
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
	if eventsAttrs == nil {
		t.Fatalf("events table carried no attributes")
	}
	if eventsAttrs["table_type"] != "TABLE" {
		t.Errorf("events table_type = %v, want TABLE", eventsAttrs["table_type"])
	}
	if eventsAttrs["kms_key_name"] != "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1" {
		t.Errorf("events kms_key_name = %v", eventsAttrs["kms_key_name"])
	}
	if len(eventsAnchors) == 0 {
		t.Errorf("events table carried no correlation anchors")
	}

	// events: dataset + kms (2). ext_sales: dataset + landing-bucket (deduped) (2).
	if relTypes[relationshipTypeBigQueryTableInDataset] != 2 {
		t.Errorf("dataset edges = %d, want 2", relTypes[relationshipTypeBigQueryTableInDataset])
	}
	if relTypes[relationshipTypeBigQueryTableKMSKey] != 1 {
		t.Errorf("kms edges = %d, want 1", relTypes[relationshipTypeBigQueryTableKMSKey])
	}
	if relTypes[relationshipTypeBigQueryTableExternalSource] != 1 {
		t.Errorf("external source edges = %d, want 1 (deduped bucket)", relTypes[relationshipTypeBigQueryTableExternalSource])
	}
}

func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}
