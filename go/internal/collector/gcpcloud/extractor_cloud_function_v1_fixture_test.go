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

// TestCloudFunctionV1OfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Functions gen1 through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes, correlation anchors, and the source-bucket / VPC-connector /
// secret / topic edges reach durable facts without any live GCP call, and that no
// raw service-account email, source object path, or https trigger URL ever lands
// on a fact.
func TestCloudFunctionV1OfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_function_v1.json"))
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
	var v1Attrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == cloudFunctionV1FullName {
				v1Attrs, _ = env.Payload["attributes"].(map[string]any)
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
	if v1Attrs == nil {
		t.Fatalf("gen1 function carried no attributes")
	}
	if v1Attrs["entry_point"] != "handler" {
		t.Errorf("entry_point = %v, want handler", v1Attrs["entry_point"])
	}
	if v1Attrs["trigger_type"] != "event" {
		t.Errorf("trigger_type = %v, want event", v1Attrs["trigger_type"])
	}
	if connectorEdges != 1 {
		t.Errorf("connector edges = %d, want 1", connectorEdges)
	}
	if secretEdges != 2 {
		t.Errorf("secret edges = %d, want 2", secretEdges)
	}
	if topicEdges != 1 {
		t.Errorf("topic edges = %d, want 1", topicEdges)
	}
	if bucketEdges != 1 {
		t.Errorf("source-bucket edges = %d, want 1", bucketEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"runtime-sa@demo-project.iam.gserviceaccount.com",
		"http-sa@demo-project.iam.gserviceaccount.com",
		"api-fn-v1-should-not-leak.zip",
		"http-fn-should-not-leak",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
