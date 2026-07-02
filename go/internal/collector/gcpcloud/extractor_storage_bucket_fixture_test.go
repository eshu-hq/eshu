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

// TestStorageBucketOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Storage Bucket through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the typed-depth attributes,
// correlation anchors, and the CMEK/logging edges reach the durable facts
// without any live GCP call.
func TestStorageBucketOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_storage_bucket.json"))
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
	var demoAttrs map[string]any
	var demoAnchors []any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == "//storage.googleapis.com/projects/_/buckets/demo-bucket" {
				demoAttrs, _ = env.Payload["attributes"].(map[string]any)
				demoAnchors, _ = env.Payload["correlation_anchors"].([]any)
				if demoAnchors == nil {
					if s, ok := env.Payload["correlation_anchors"].([]string); ok {
						for _, v := range s {
							demoAnchors = append(demoAnchors, v)
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
	if demoAttrs == nil {
		t.Fatalf("demo-bucket carried no attributes")
	}
	if demoAttrs["storage_class"] != "STANDARD" {
		t.Errorf("demo-bucket storage_class = %v, want STANDARD", demoAttrs["storage_class"])
	}
	if demoAttrs["kms_key_name"] != "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1" {
		t.Errorf("demo-bucket kms_key_name = %v", demoAttrs["kms_key_name"])
	}
	if demoAttrs["uniform_bucket_level_access"] != true {
		t.Errorf("demo-bucket uniform_bucket_level_access = %v, want true", demoAttrs["uniform_bucket_level_access"])
	}
	if demoAttrs["public_access_prevention"] != "enforced" {
		t.Errorf("demo-bucket public_access_prevention = %v, want enforced", demoAttrs["public_access_prevention"])
	}
	if len(demoAnchors) == 0 {
		t.Errorf("demo-bucket carried no correlation anchors")
	}

	// Only demo-bucket has KMS/logging configured; raw-bucket has neither.
	if relTypes[relationshipTypeStorageBucketKMSKey] != 1 {
		t.Errorf("kms edges = %d, want 1", relTypes[relationshipTypeStorageBucketKMSKey])
	}
	if relTypes[relationshipTypeStorageBucketLogsToBucket] != 1 {
		t.Errorf("logging edges = %d, want 1", relTypes[relationshipTypeStorageBucketLogsToBucket])
	}
}
