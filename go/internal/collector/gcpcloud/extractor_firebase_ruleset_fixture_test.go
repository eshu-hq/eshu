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

const firebaseRulesetFullName = "//firebaserules.googleapis.com/projects/demo-project/rulesets/abc123"

// TestFirebaseRulesetOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Firebase Rules Ruleset through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the parent-project edges reach durable facts without any live
// GCP call, and that no raw rule source content lands on a fact.
func TestFirebaseRulesetOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_firebase_ruleset.json"))
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
	var firestoreAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == firebaseRulesetFullName {
				firestoreAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeFirebaseRulesetBelongsToProject {
				projectEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if firestoreAttrs == nil {
		t.Fatalf("firestore ruleset attributes missing")
	}
	if firestoreAttrs["source_file_count"] != float64(2) && firestoreAttrs["source_file_count"] != 2 {
		t.Errorf("source_file_count = %v, want 2", firestoreAttrs["source_file_count"])
	}
	if projectEdges != 2 {
		t.Errorf("firebase_ruleset_belongs_to_project edges = %d, want 2", projectEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"rules_version", "isSignedIn", "request.auth", "service cloud.firestore"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked raw rule source token %q", token)
		}
	}
}
