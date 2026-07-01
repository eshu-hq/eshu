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

// TestRouteOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// compute Route through parse -> normalize -> attribute extraction -> generation
// -> envelope, proving the redaction-safe typed-depth attributes, correlation
// anchors, and the network/next-hop edges reach durable facts without any live
// GCP call, and that no destination CIDR address leaks.
func TestRouteOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_route.json"))
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
	networkEdges := 0
	instanceEdges := 0
	var defaultAttrs map[string]any
	const defaultRouteFullName = "//compute.googleapis.com/projects/demo-project/global/routes/default-route"
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == defaultRouteFullName {
				defaultAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeRouteInNetwork:
				networkEdges++
			case relationshipTypeRouteNextHopInstance:
				instanceEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if defaultAttrs == nil {
		t.Fatalf("default-route carried no attributes")
	}
	if defaultAttrs["dest_is_default"] != true {
		t.Errorf("default-route dest_is_default = %v, want true", defaultAttrs["dest_is_default"])
	}
	if defaultAttrs["next_hop_gateway"] != "default-internet-gateway" {
		t.Errorf("default-route next_hop_gateway = %v, want default-internet-gateway", defaultAttrs["next_hop_gateway"])
	}
	// Both routes reference the network -> 2; to-instance has 1 instance next hop.
	if networkEdges != 2 {
		t.Errorf("route_in_network edges = %d, want 2", networkEdges)
	}
	if instanceEdges != 1 {
		t.Errorf("route_next_hop_instance edges = %d, want 1", instanceEdges)
	}

	// The destination CIDR address must never reach a fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if containsString(string(blob), "10.128.0.0") {
		t.Fatalf("envelope set leaked destination CIDR address")
	}
}
