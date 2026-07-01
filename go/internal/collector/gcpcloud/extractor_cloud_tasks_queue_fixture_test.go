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

// TestCloudTasksQueueOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Tasks Queue through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the App Engine service target edge reach durable facts without
// any live GCP call, and that no HTTP override path/audience or raw
// service-account email ever lands on a fact.
func TestCloudTasksQueueOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_tasks_queue.json"))
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
	relationshipCount := 0
	var queueAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == cloudTasksQueueFullName {
				queueAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			relationshipCount++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if queueAttrs == nil {
		t.Fatalf("app-engine queue carried no attributes")
	}
	if queueAttrs["state"] != "RUNNING" {
		t.Errorf("state = %v, want RUNNING", queueAttrs["state"])
	}
	if queueAttrs["app_engine_routing_service"] != "worker" {
		t.Errorf("app_engine_routing_service = %v, want worker", queueAttrs["app_engine_routing_service"])
	}
	// The queue emits no typed edge (its numeric project number cannot resolve to
	// an App Engine application id); the routing service is a bounded attribute.
	if relationshipCount != 0 {
		t.Errorf("cloud tasks queue relationship facts = %d, want 0", relationshipCount)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"tasks-sa@demo-project.iam.gserviceaccount.com",
		"api.internal.example.com",
		"should-not-leak",
		"should-not-leak-audience",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
