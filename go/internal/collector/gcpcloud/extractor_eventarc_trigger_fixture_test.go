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

// TestEventarcTriggerOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Eventarc Trigger through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the destination / transport / channel edges reach
// durable facts without any live GCP call, and that no cloudRun path or raw
// service-account email ever lands on a fact.
func TestEventarcTriggerOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_eventarc_trigger.json"))
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
	serviceEdges := 0
	functionEdges := 0
	topicEdges := 0
	channelEdges := 0
	var runAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == eventarcTriggerFullName {
				runAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeTriggerTargetsService:
				serviceEdges++
			case relationshipTypeTriggerTargetsFunction:
				functionEdges++
			case relationshipTypeTriggerTransportTopic:
				topicEdges++
			case relationshipTypeTriggerUsesChannel:
				channelEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if runAttrs == nil {
		t.Fatalf("run trigger carried no attributes")
	}
	if runAttrs["destination_type"] != "run" {
		t.Errorf("destination_type = %v, want run", runAttrs["destination_type"])
	}
	if serviceEdges != 1 {
		t.Errorf("trigger_targets_service edges = %d, want 1", serviceEdges)
	}
	if functionEdges != 1 {
		t.Errorf("trigger_targets_function edges = %d, want 1", functionEdges)
	}
	if topicEdges != 1 {
		t.Errorf("trigger_transport_topic edges = %d, want 1", topicEdges)
	}
	if channelEdges != 1 {
		t.Errorf("trigger_uses_channel edges = %d, want 1", channelEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"eventarc-sa@demo-project.iam.gserviceaccount.com", "events-should-not-leak"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
