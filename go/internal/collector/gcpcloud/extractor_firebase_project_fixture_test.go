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

const firebaseProjectFullName = "//firebase.googleapis.com/projects/demo-project"

// TestFirebaseProjectOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Firebase Project through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes and
// the backing-project and default-bucket edges reach durable facts without any
// live GCP call.
func TestFirebaseProjectOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_firebase_project.json"))
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
	bucketEdges := 0
	var demoAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == firebaseProjectFullName {
				demoAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeFirebaseProjectBackedByProject:
				projectEdges++
			case relationshipTypeFirebaseProjectDefaultBucket:
				bucketEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if demoAttrs == nil {
		t.Fatalf("demo firebase project attributes missing")
	}
	if demoAttrs["state"] != "ACTIVE" {
		t.Errorf("demo state = %v, want ACTIVE", demoAttrs["state"])
	}
	if demoAttrs["default_storage_bucket_present"] != true {
		t.Errorf("demo default_storage_bucket_present = %v, want true", demoAttrs["default_storage_bucket_present"])
	}
	// Both projects carry projectNumber, so both mint a backing-project edge; only
	// the demo project declares a default storage bucket.
	if projectEdges != 2 {
		t.Errorf("firebase_project_backed_by_project edges = %d, want 2", projectEdges)
	}
	if bucketEdges != 1 {
		t.Errorf("firebase_project_default_bucket edges = %d, want 1", bucketEdges)
	}
}
