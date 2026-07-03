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

// TestURLMapOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for compute UrlMap through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and default-service/path-matcher/path-rule backend
// edges reach durable facts without any live GCP call, and that no raw host
// or path routing pattern ever lands on a fact.
func TestURLMapOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_url_map.json"))
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
	relTypeCounts := map[string]int{}
	var webMapAttrs map[string]any
	cdnMapSeen := false
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			switch env.Payload["full_resource_name"] {
			case "//compute.googleapis.com/projects/demo-project/global/urlMaps/web-map":
				webMapAttrs, _ = env.Payload["attributes"].(map[string]any)
			case "//compute.googleapis.com/projects/demo-project/global/urlMaps/cdn-map":
				cdnMapSeen = true
			}
		case facts.GCPCloudRelationshipFactKind:
			relTypeCounts[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if webMapAttrs == nil {
		t.Fatalf("web-map carried no attributes")
	}
	if webMapAttrs["host_rule_count"] != float64(1) && webMapAttrs["host_rule_count"] != 1 {
		t.Errorf("web-map host_rule_count = %v, want 1", webMapAttrs["host_rule_count"])
	}
	if webMapAttrs["path_matcher_count"] != float64(1) && webMapAttrs["path_matcher_count"] != 1 {
		t.Errorf("web-map path_matcher_count = %v, want 1", webMapAttrs["path_matcher_count"])
	}
	// cdn-map carries only a defaultService BackendBucket edge and no other
	// decoded field, so its attributes map is legitimately empty (omitted as
	// nil by cloneAnyMap) — its resource fact must still exist.
	if !cdnMapSeen {
		t.Fatalf("cdn-map gcp_cloud_resource fact not found")
	}

	if relTypeCounts[relationshipTypeURLMapDefaultService] != 2 {
		t.Errorf("default-service edges = %d, want 2", relTypeCounts[relationshipTypeURLMapDefaultService])
	}
	if relTypeCounts[relationshipTypeURLMapPathMatcherService] != 1 {
		t.Errorf("path-matcher edges = %d, want 1", relTypeCounts[relationshipTypeURLMapPathMatcherService])
	}
	if relTypeCounts[relationshipTypeURLMapPathRuleService] != 1 {
		t.Errorf("path-rule edges = %d, want 1", relTypeCounts[relationshipTypeURLMapPathRuleService])
	}

	// No raw host or path routing pattern from the fixture may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"example.com", "www.example.com", "/images/*"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked routing pattern %q", token)
		}
	}
}
