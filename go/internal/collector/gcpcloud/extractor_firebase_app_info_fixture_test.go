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

const firebaseAppInfoFullName = "//firebase.googleapis.com/projects/demo-project/webApps/1:123456789:web:abc123"

// TestFirebaseAppInfoOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Firebase App Info through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes and
// the parent-project edges reach durable facts without any live GCP call.
func TestFirebaseAppInfoOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_firebase_app_info.json"))
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
	projectEdges := 0
	var webAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == firebaseAppInfoFullName {
				webAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeFirebaseAppBelongsToProject {
				projectEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if webAttrs == nil {
		t.Fatalf("web app attributes missing")
	}
	if webAttrs["platform"] != "WEB" {
		t.Errorf("web app platform = %v, want WEB", webAttrs["platform"])
	}
	if webAttrs["app_id"] != "1:123456789:web:abc123" {
		t.Errorf("web app app_id = %v", webAttrs["app_id"])
	}
	// Both apps live under demo-project, so each mints one parent-project edge.
	if projectEdges != 2 {
		t.Errorf("firebase_app_belongs_to_project edges = %d, want 2", projectEdges)
	}
}
