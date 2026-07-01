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

// TestServiceAccountKeyOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for IAM ServiceAccountKey through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the parent-ServiceAccount edge reach durable facts without any
// live GCP call, and that no key material ever lands on a fact.
func TestServiceAccountKeyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_service_account_key.json"))
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
	parentEdges := 0
	var firstKeyAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == serviceAccountKeyFullName {
				firstKeyAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeServiceAccountKeyOf {
				parentEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if firstKeyAttrs == nil {
		t.Fatalf("abc123 key carried no attributes")
	}
	if firstKeyAttrs["key_type"] != "USER_MANAGED" {
		t.Errorf("abc123 key_type = %v, want USER_MANAGED", firstKeyAttrs["key_type"])
	}
	if firstKeyAttrs["parent_service_account_email_fingerprint"] == nil {
		t.Errorf("abc123 missing parent fingerprint: %#v", firstKeyAttrs)
	}
	if parentEdges != 2 {
		t.Errorf("service_account_key_of edges = %d, want 2", parentEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"privateKeyData", "publicKeyData", "privateKeyType"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked key-material token %q", token)
		}
	}
}
