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

// TestRecaptchaKeyOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for reCAPTCHA Enterprise Key through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes reach durable facts without any live GCP call, and that no platform
// allow-list identifier (domain, package name, bundle id) ever lands on a fact.
func TestRecaptchaKeyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_recaptcha_key.json"))
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
	var webAttrs map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		if env.Payload["full_resource_name"] == recaptchaKeyFullName {
			webAttrs, _ = env.Payload["attributes"].(map[string]any)
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if webAttrs == nil || webAttrs["platform_type"] != "web" {
		t.Errorf("web key attrs missing/incorrect: %#v", webAttrs)
	}
	if webAttrs["integration_type"] != "SCORE" {
		t.Errorf("web key integration_type = %v, want SCORE", webAttrs["integration_type"])
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"example.com", "app.example.com", "com.example.app", "allowedDomains", "allowedPackageNames"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked platform identifier token %q", token)
		}
	}
}
