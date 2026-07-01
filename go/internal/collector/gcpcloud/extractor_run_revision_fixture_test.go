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

// TestRunRevisionOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Cloud Run Revision through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the parent-service / VPC-connector / mounted-secret
// edges reach durable facts without any live GCP call, and that no env value or
// raw runtime service-account email ever lands on a fact.
func TestRunRevisionOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_run_revision.json"))
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
	connectorEdges := 0
	secretEdges := 0
	var apiAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == runRevisionFullName {
				apiAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeRevisionOfService:
				parentEdges++
			case relationshipTypeRevisionUsesVPCConnector:
				connectorEdges++
			case relationshipTypeRevisionMountsSecret:
				secretEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if apiAttrs == nil {
		t.Fatalf("api-service revision carried no attributes")
	}
	if apiAttrs["container_image_digest"] != runRevisionImageDigest {
		t.Errorf("container_image_digest = %v, want %s", apiAttrs["container_image_digest"], runRevisionImageDigest)
	}
	if apiAttrs["ready_condition_state"] != "CONDITION_SUCCEEDED" {
		t.Errorf("ready_condition_state = %v, want CONDITION_SUCCEEDED", apiAttrs["ready_condition_state"])
	}
	// Both revisions resolve a parent service from their own name.
	if parentEdges != 2 {
		t.Errorf("revision_of_service edges = %d, want 2", parentEdges)
	}
	if connectorEdges != 1 {
		t.Errorf("revision_uses_vpc_connector edges = %d, want 1", connectorEdges)
	}
	if secretEdges != 2 {
		t.Errorf("revision_mounts_secret edges = %d, want 2", secretEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"info-should-not-leak", "runtime-sa@demo-project.iam.gserviceaccount.com"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
