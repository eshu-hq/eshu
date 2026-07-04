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

// TestSecurityPolicyOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Armor SecurityPolicy through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes reach durable facts without any live GCP call, and
// that no rule match expression, IP/CIDR value, or free-text description ever
// lands on a fact.
func TestSecurityPolicyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_security_policy.json"))
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

	const globalPolicyName = "//compute.googleapis.com/projects/demo-project/global/securityPolicies/armor-policy"
	const regionalPolicyName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/securityPolicies/regional-armor-policy"

	resourceCount := 0
	var globalAttrs, regionalAttrs map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		switch env.Payload["full_resource_name"] {
		case globalPolicyName:
			globalAttrs, _ = env.Payload["attributes"].(map[string]any)
		case regionalPolicyName:
			regionalAttrs, _ = env.Payload["attributes"].(map[string]any)
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if globalAttrs == nil {
		t.Fatalf("armor-policy carried no attributes")
	}
	if globalAttrs["type"] != "CLOUD_ARMOR" {
		t.Errorf("armor-policy type = %v, want CLOUD_ARMOR", globalAttrs["type"])
	}
	if globalAttrs["rule_count"] != 2 {
		t.Errorf("armor-policy rule_count = %v, want 2", globalAttrs["rule_count"])
	}
	if globalAttrs["adaptive_protection_enabled"] != true {
		t.Errorf("armor-policy adaptive_protection_enabled = %v, want true", globalAttrs["adaptive_protection_enabled"])
	}
	if _, ok := globalAttrs["region"]; ok {
		t.Errorf("global policy must omit region: %#v", globalAttrs)
	}

	if regionalAttrs == nil {
		t.Fatalf("regional-armor-policy carried no attributes")
	}
	if regionalAttrs["region"] != "us-central1" {
		t.Errorf("regional-armor-policy region = %v, want us-central1", regionalAttrs["region"])
	}

	// No rule match expression, IP/CIDR value, or free-text description must ever
	// reach a fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"SRC_IPS_V1", "203.0.113.0/24", "Blocks known bad actors", "Block bad IPs"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked raw token %q", token)
		}
	}
}
