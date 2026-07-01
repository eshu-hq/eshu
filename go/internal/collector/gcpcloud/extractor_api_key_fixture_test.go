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

// TestAPIKeyOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// API Key through parse -> normalize -> attribute extraction -> generation ->
// envelope, proving the redaction-safe typed-depth attributes reach durable facts
// without any live GCP call, and that neither the secret key string nor any
// restriction value (IPs, referrers) ever lands on a fact.
func TestAPIKeyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_api_key.json"))
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
	var demoAttrs map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		if env.Payload["full_resource_name"] == apiKeyFullName {
			demoAttrs, _ = env.Payload["attributes"].(map[string]any)
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if demoAttrs == nil || demoAttrs["restriction_type"] != "server" {
		t.Errorf("demo key attrs missing/incorrect: %#v", demoAttrs)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"keyString", "AIzaSy-PLACEHOLDER", "203.0.113.4", "allowedIps"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked sensitive token %q", token)
		}
	}
}
