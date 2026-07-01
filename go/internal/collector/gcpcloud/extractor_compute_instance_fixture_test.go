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

// TestInstanceOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for compute Instance through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the disk/network/subnetwork edges reach durable facts
// without any live GCP call, and that no IP address or metadata value leaks.
func TestInstanceOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_compute_instance.json"))
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
	diskEdges := 0
	networkEdges := 0
	subnetworkEdges := 0
	var webAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == instanceFullName {
				webAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeInstanceUsesDisk:
				diskEdges++
			case relationshipTypeInstanceInNetwork:
				networkEdges++
			case relationshipTypeInstanceInSubnetwork:
				subnetworkEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if webAttrs == nil {
		t.Fatalf("web-1 carried no attributes")
	}
	if webAttrs["status"] != "RUNNING" {
		t.Errorf("web-1 status = %v, want RUNNING", webAttrs["status"])
	}
	if webAttrs["machine_type"] != "e2-standard-4" {
		t.Errorf("web-1 machine_type = %v, want e2-standard-4", webAttrs["machine_type"])
	}
	if webAttrs["has_external_ip"] != true {
		t.Errorf("web-1 has_external_ip = %v, want true", webAttrs["has_external_ip"])
	}
	// web-1: 1 disk, 1 network, 1 subnetwork. batch-1: 1 disk, 1 network.
	if diskEdges != 2 {
		t.Errorf("instance_uses_disk edges = %d, want 2", diskEdges)
	}
	if networkEdges != 2 {
		t.Errorf("instance_in_network edges = %d, want 2", networkEdges)
	}
	if subnetworkEdges != 1 {
		t.Errorf("instance_in_subnetwork edges = %d, want 1", subnetworkEdges)
	}

	// No IP address (public or private) or metadata value may reach a fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, forbidden := range []string{"203.0.113.7", "10.0.0.5", "10.0.0.9", "#!/bin/bash"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("envelope set leaked forbidden value %q", forbidden)
		}
	}
}
