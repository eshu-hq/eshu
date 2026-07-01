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

// TestAppEngineApplicationOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for App Engine Application through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes and the default-bucket edge reach durable facts
// without any live GCP call.
func TestAppEngineApplicationOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_app_engine_application.json"))
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
	bucketEdges := 0
	var appAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == appEngineApplicationFullName {
				appAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeAppEngineApplicationUsesDefaultBucket {
				bucketEdges++
			}
		}
	}

	if resourceCount != 1 {
		t.Errorf("gcp_cloud_resource facts = %d, want 1", resourceCount)
	}
	if appAttrs == nil {
		t.Fatalf("app engine application carried no attributes")
	}
	if appAttrs["location_id"] != "us-central" {
		t.Errorf("location_id = %v, want us-central", appAttrs["location_id"])
	}
	if appAttrs["serving_status"] != "SERVING" {
		t.Errorf("serving_status = %v, want SERVING", appAttrs["serving_status"])
	}
	if appAttrs["default_bucket"] != "staging.demo-project.appspot.com" {
		t.Errorf("default_bucket = %v, want staging.demo-project.appspot.com", appAttrs["default_bucket"])
	}
	if appAttrs["default_hostname"] != "demo-project.uc.r.appspot.com" {
		t.Errorf("default_hostname = %v, want demo-project.uc.r.appspot.com", appAttrs["default_hostname"])
	}
	if appAttrs["database_type"] != "CLOUD_FIRESTORE" {
		t.Errorf("database_type = %v, want CLOUD_FIRESTORE", appAttrs["database_type"])
	}
	if appAttrs["creation_time"] != "2024-01-01T00:00:00Z" {
		t.Errorf("creation_time = %v, want 2024-01-01T00:00:00Z", appAttrs["creation_time"])
	}
	if bucketEdges != 1 {
		t.Errorf("application_uses_default_bucket edges = %d, want 1", bucketEdges)
	}
}
