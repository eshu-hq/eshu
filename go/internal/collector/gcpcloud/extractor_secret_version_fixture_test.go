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

// TestSecretVersionOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Secret Manager Secret Version through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes and the parent-secret + KMS-key-version edges reach
// durable facts without any live GCP call, and that no secret payload ever lands
// on a fact.
func TestSecretVersionOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_secret_version.json"))
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
	kmsEdges := 0
	var enabledAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == secretVersionFullName {
				enabledAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeSecretVersionOfSecret:
				parentEdges++
			case relationshipTypeSecretVersionEncryptedByKMSKeyVersion:
				kmsEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if enabledAttrs == nil || enabledAttrs["state"] != "ENABLED" {
		t.Errorf("enabled version attrs missing/incorrect: %#v", enabledAttrs)
	}
	if enabledAttrs["customer_managed_encryption"] != true {
		t.Errorf("enabled version customer_managed_encryption = %v, want true", enabledAttrs["customer_managed_encryption"])
	}
	if parentEdges != 2 {
		t.Errorf("secret_version_of_secret edges = %d, want 2", parentEdges)
	}
	if kmsEdges != 1 {
		t.Errorf("secret_version_encrypted_by_kms_key_version edges = %d, want 1", kmsEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"payload", "secretData"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked payload token %q", token)
		}
	}
}
