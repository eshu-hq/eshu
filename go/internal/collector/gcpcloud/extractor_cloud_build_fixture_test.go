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

// TestCloudBuildOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Cloud Build through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the trigger / source-repo / source-bucket edges reach
// durable facts without any live GCP call, and that no build substitution value,
// source object path, or raw service-account email ever lands on a fact.
func TestCloudBuildOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_build.json"))
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
	triggerEdges := 0
	repoEdges := 0
	bucketEdges := 0
	var buildAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == cloudBuildFullName {
				buildAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeBuildTriggeredBy:
				triggerEdges++
			case relationshipTypeBuildSourceRepo:
				repoEdges++
			case relationshipTypeBuildSourceBucket:
				bucketEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if buildAttrs == nil {
		t.Fatalf("build carried no attributes")
	}
	if buildAttrs["status"] != "SUCCESS" {
		t.Errorf("status = %v, want SUCCESS", buildAttrs["status"])
	}
	if buildAttrs["source_type"] != "repo" {
		t.Errorf("source_type = %v, want repo", buildAttrs["source_type"])
	}
	if triggerEdges != 1 {
		t.Errorf("build_triggered_by edges = %d, want 1", triggerEdges)
	}
	if repoEdges != 1 {
		t.Errorf("build_source_repo edges = %d, want 1", repoEdges)
	}
	if bucketEdges != 1 {
		t.Errorf("build_source_bucket edges = %d, want 1 (from the minimal build)", bucketEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"should-not-leak-value",
		"_DEPLOY_SECRET",
		"source-should-not-leak.tgz",
		"build-sa@demo-project.iam.gserviceaccount.com",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
