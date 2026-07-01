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

// TestIdentityConfigOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Identity Platform Config through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes reach durable facts without any live GCP call, and that no
// authorized-domain value or client secret ever lands on a fact.
func TestIdentityConfigOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_identity_platform_config.json"))
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

	var attrs map[string]any
	resourceCount := 0
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		if env.Payload["full_resource_name"] == identityConfigFullName {
			attrs, _ = env.Payload["attributes"].(map[string]any)
		}
	}

	if resourceCount != 1 {
		t.Errorf("gcp_cloud_resource facts = %d, want 1", resourceCount)
	}
	if attrs == nil || attrs["mfa_state"] != "ENABLED" {
		t.Errorf("config attrs missing/incorrect: %#v", attrs)
	}
	if attrs["authorized_domain_count"] != float64(3) && attrs["authorized_domain_count"] != 3 {
		t.Errorf("authorized_domain_count = %v, want 3", attrs["authorized_domain_count"])
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"demo.firebaseapp.com", "demo.web.app", "localhost", "authorizedDomains"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked authorized-domain token %q", token)
		}
	}
}
