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

// TestSecretManagerSecretOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for Secret Manager Secret through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes, correlation anchors, and the KMS/Pub-Sub edges reach
// durable facts without any live GCP call, and that no secret payload ever
// lands on a fact.
func TestSecretManagerSecretOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_secret_manager_secret.json"))
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
	kmsEdges := 0
	topicEdges := 0
	var apiTokenAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == secretManagerSecretFullName {
				apiTokenAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeSecretEncryptedByKMSKey:
				kmsEdges++
			case relationshipTypeSecretNotifiesTopic:
				topicEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if apiTokenAttrs == nil {
		t.Fatalf("api-token secret carried no attributes")
	}
	if apiTokenAttrs["replication_type"] != "user_managed" {
		t.Errorf("api-token replication_type = %v, want user_managed", apiTokenAttrs["replication_type"])
	}
	if apiTokenAttrs["customer_managed_encryption"] != true {
		t.Errorf("api-token customer_managed_encryption = %v, want true", apiTokenAttrs["customer_managed_encryption"])
	}
	if kmsEdges != 2 {
		t.Errorf("secret_encrypted_by_kms_key edges = %d, want 2", kmsEdges)
	}
	if topicEdges != 1 {
		t.Errorf("secret_notifies_topic edges = %d, want 1", topicEdges)
	}

	// No secret payload or value token may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"payload", "secretData", "super-secret"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked payload token %q", token)
		}
	}
}
