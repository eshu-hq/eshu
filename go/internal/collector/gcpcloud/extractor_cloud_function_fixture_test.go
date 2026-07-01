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

// TestCloudFunctionOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Functions Function (gen2 + gen1) through parse -> normalize
// -> attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes, correlation anchors, and the connector / secret /
// topic / source-bucket edges reach durable facts without any live GCP call, and
// that no raw service-account email, source object path, or https trigger URL
// ever lands on a fact.
func TestCloudFunctionOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_function.json"))
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
	connectorEdges := 0
	secretEdges := 0
	topicEdges := 0
	bucketEdges := 0
	var gen2Attrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == cloudFunctionFullName {
				gen2Attrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeFunctionUsesVPCConnector:
				connectorEdges++
			case relationshipTypeFunctionMountsSecret:
				secretEdges++
			case relationshipTypeFunctionTriggeredByTopic:
				topicEdges++
			case relationshipTypeFunctionSourceBucket:
				bucketEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if gen2Attrs == nil {
		t.Fatalf("gen2 function carried no attributes")
	}
	if gen2Attrs["environment"] != "GEN_2" {
		t.Errorf("environment = %v, want GEN_2", gen2Attrs["environment"])
	}
	if gen2Attrs["event_type"] != "google.cloud.pubsub.topic.v1.messagePublished" {
		t.Errorf("event_type = %v", gen2Attrs["event_type"])
	}
	if connectorEdges != 1 {
		t.Errorf("function_uses_vpc_connector edges = %d, want 1", connectorEdges)
	}
	if secretEdges != 2 {
		t.Errorf("function_mounts_secret edges = %d, want 2", secretEdges)
	}
	if topicEdges != 1 {
		t.Errorf("function_triggered_by_topic edges = %d, want 1", topicEdges)
	}
	// gen2 storageSource bucket + gen1 sourceArchiveUrl bucket = 2.
	if bucketEdges != 2 {
		t.Errorf("function_source_bucket edges = %d, want 2", bucketEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"runtime-sa@demo-project.iam.gserviceaccount.com",
		"trigger-sa@demo-project.iam.gserviceaccount.com",
		"legacy-sa@demo-project.iam.gserviceaccount.com",
		"legacy-fn-should-not-leak",
		"api-fn-source.zip",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
