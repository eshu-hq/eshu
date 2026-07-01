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

// TestRunServiceOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Cloud Run Service through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the VPC-connector and mounted-secret edges reach
// durable facts without any live GCP call, and that no env value ever lands on a
// fact.
func TestRunServiceOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_run_service.json"))
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
	var apiAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == runServiceFullName {
				apiAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeRunServiceUsesVPCConnector:
				connectorEdges++
			case relationshipTypeRunServiceMountsSecret:
				secretEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if apiAttrs == nil {
		t.Fatalf("api-service carried no attributes")
	}
	if apiAttrs["ingress"] != "INGRESS_TRAFFIC_INTERNAL_ONLY" {
		t.Errorf("ingress = %v, want INGRESS_TRAFFIC_INTERNAL_ONLY", apiAttrs["ingress"])
	}
	if apiAttrs["vpc_egress"] != "ALL_TRAFFIC" {
		t.Errorf("vpc_egress = %v, want ALL_TRAFFIC", apiAttrs["vpc_egress"])
	}
	if connectorEdges != 1 {
		t.Errorf("run_service_uses_vpc_connector edges = %d, want 1", connectorEdges)
	}
	if secretEdges != 2 {
		t.Errorf("run_service_mounts_secret edges = %d, want 2", secretEdges)
	}

	// No env value or raw runtime service-account email may reach any fact.
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
