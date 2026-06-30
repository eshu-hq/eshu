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

// TestComputeNetworkOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for VPC Network through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the typed-depth attributes, correlation
// anchors, and typed subnetwork/peering edges reach the durable facts without any
// live GCP call.
func TestComputeNetworkOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_compute_network.json"))
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
	relTypes := map[string]int{}
	var prodAttrs map[string]any
	var prodAnchors []any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc" {
				prodAttrs, _ = env.Payload["attributes"].(map[string]any)
				if s, ok := env.Payload["correlation_anchors"].([]string); ok {
					for _, v := range s {
						prodAnchors = append(prodAnchors, v)
					}
				}
			}
		case facts.GCPCloudRelationshipFactKind:
			relTypes[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if prodAttrs == nil {
		t.Fatalf("prod-vpc carried no attributes")
	}
	if prodAttrs["routing_mode"] != "GLOBAL" {
		t.Errorf("prod-vpc routing_mode = %v, want GLOBAL", prodAttrs["routing_mode"])
	}
	if prodAttrs["auto_create_subnetworks"] != false {
		t.Errorf("prod-vpc auto_create_subnetworks = %v, want false", prodAttrs["auto_create_subnetworks"])
	}
	if len(prodAnchors) == 0 {
		t.Errorf("prod-vpc carried no correlation anchors")
	}

	// prod-vpc: 2 subnet edges + 1 peering edge. default (auto-mode): none.
	if relTypes[relationshipTypeNetworkContainsSubnetwork] != 2 {
		t.Errorf("subnetwork edges = %d, want 2", relTypes[relationshipTypeNetworkContainsSubnetwork])
	}
	if relTypes[relationshipTypeNetworkPeersWithNetwork] != 1 {
		t.Errorf("peering edges = %d, want 1", relTypes[relationshipTypeNetworkPeersWithNetwork])
	}
}
