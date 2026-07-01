// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestPubSubTopicOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Pub/Sub Topic through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the KMS/Schema edges reach durable facts without any
// live GCP call.
func TestPubSubTopicOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_pubsub_topic.json"))
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
	schemaEdges := 0
	var ordersAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == pubSubTopicFullName {
				ordersAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeTopicEncryptedByKMSKey:
				kmsEdges++
			case relationshipTypeTopicUsesSchema:
				schemaEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if ordersAttrs == nil {
		t.Fatalf("orders topic carried no attributes")
	}
	if ordersAttrs["state"] != "ACTIVE" {
		t.Errorf("orders state = %v, want ACTIVE", ordersAttrs["state"])
	}
	if ordersAttrs["customer_managed_encryption"] != true {
		t.Errorf("orders customer_managed_encryption = %v, want true", ordersAttrs["customer_managed_encryption"])
	}
	if ordersAttrs["schema_encoding"] != "JSON" {
		t.Errorf("orders schema_encoding = %v, want JSON", ordersAttrs["schema_encoding"])
	}
	if kmsEdges != 1 {
		t.Errorf("topic_encrypted_by_kms_key edges = %d, want 1", kmsEdges)
	}
	if schemaEdges != 1 {
		t.Errorf("topic_uses_schema edges = %d, want 1", schemaEdges)
	}
}
