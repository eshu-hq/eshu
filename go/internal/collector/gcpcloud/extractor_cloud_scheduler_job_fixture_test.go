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

// TestCloudSchedulerJobOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Cloud Scheduler Job through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the Pub/Sub target edge reach durable facts without any live GCP
// call, and that no pubsub payload, http target URI/headers/audience, or raw
// service-account email ever lands on a fact.
func TestCloudSchedulerJobOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_scheduler_job.json"))
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
	topicEdges := 0
	var pubsubAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == cloudSchedulerJobFullName {
				pubsubAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeSchedulerJobTargetsTopic {
				topicEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if pubsubAttrs == nil {
		t.Fatalf("pubsub job carried no attributes")
	}
	if pubsubAttrs["target_type"] != "pubsub" {
		t.Errorf("target_type = %v, want pubsub", pubsubAttrs["target_type"])
	}
	if topicEdges != 1 {
		t.Errorf("scheduler_job_targets_topic edges = %d, want 1", topicEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"scheduler-sa@demo-project.iam.gserviceaccount.com",
		"api.internal.example.com",
		"should-not-leak-token",
		"should-not-leak-header",
		"should-not-leak-audience",
		"c2VjcmV0LXBheWxvYWQtc2hvdWxkLW5vdC1sZWFr",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
