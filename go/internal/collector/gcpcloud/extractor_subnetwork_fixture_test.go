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

// TestSubnetworkOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for compute Subnetwork through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the parent-network edge reach durable facts without
// any live GCP call, and that no IP address or CIDR ever lands on a fact.
func TestSubnetworkOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_subnetwork.json"))
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
	var privateAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == subnetworkFullName {
				privateAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeSubnetworkInNetwork {
				networkEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if privateAttrs == nil {
		t.Fatalf("private-subnet carried no attributes")
	}
	if privateAttrs["purpose"] != "PRIVATE" {
		t.Errorf("private-subnet purpose = %v, want PRIVATE", privateAttrs["purpose"])
	}
	if privateAttrs["ip_cidr_prefix_length"] == nil {
		t.Errorf("private-subnet carried no ip_cidr_prefix_length")
	}
	// Both subnets reference the same parent network.
	if networkEdges != 2 {
		t.Errorf("subnetwork_in_network edges = %d, want 2", networkEdges)
	}

	// No IP address, gateway, or CIDR token from the fixture may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"10.0.0.0", "10.0.0.1", "10.4.0.0", "10.1.0.0", "10.2.0.0"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked IP token %q", token)
		}
	}
}
