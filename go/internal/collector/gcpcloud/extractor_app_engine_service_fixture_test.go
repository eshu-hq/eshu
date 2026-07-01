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

// TestAppEngineServiceOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for App Engine Service through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and version edges reach durable facts without any live GCP call.
func TestAppEngineServiceOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_app_engine_service.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(page.Resources))
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
	relationshipCount := 0
	var serviceAttrs map[string]any
	const wantFullName = "//appengine.googleapis.com/apps/demo-project/services/default"
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == wantFullName {
				serviceAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			relationshipCount++
		}
	}

	if resourceCount != 1 {
		t.Errorf("gcp_cloud_resource facts = %d, want 1", resourceCount)
	}
	// Two version allocations (v1 + v2) produce two relationship facts.
	if relationshipCount != 2 {
		t.Errorf("gcp_cloud_relationship facts = %d, want 2 (one per version allocation)", relationshipCount)
	}
	if serviceAttrs == nil {
		t.Fatalf("service carried no attributes")
	}
	if serviceAttrs["service_id"] != "default" {
		t.Errorf("service_id = %v, want default", serviceAttrs["service_id"])
	}
	if serviceAttrs["split_shard_by"] != "IP" {
		t.Errorf("split_shard_by = %v, want IP", serviceAttrs["split_shard_by"])
	}
	if serviceAttrs["version_count"] != 2 {
		t.Errorf("version_count = %v, want 2", serviceAttrs["version_count"])
	}
}
