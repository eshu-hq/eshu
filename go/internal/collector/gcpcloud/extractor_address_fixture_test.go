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

// TestAddressOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// compute Static Address through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the network/subnetwork/used-by edges reach durable
// facts without any live GCP call, and that no reserved IP address leaks.
func TestAddressOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_address.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(page.Resources))
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
	forwardingRuleEdges := 0
	instanceEdges := 0
	networkEdges := 0
	subnetworkEdges := 0
	var webAttrs map[string]any
	const webIPFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/addresses/web-ip"
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == webIPFullName {
				attrs, ok := env.Payload["attributes"].(map[string]any)
				if !ok {
					t.Fatalf("web-ip attributes payload has unexpected shape %T", env.Payload["attributes"])
				}
				webAttrs = attrs
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeAddressUsedByForwardingRule:
				forwardingRuleEdges++
			case relationshipTypeAddressUsedByInstance:
				instanceEdges++
			case relationshipTypeAddressInNetwork:
				networkEdges++
			case relationshipTypeAddressInSubnetwork:
				subnetworkEdges++
			}
		}
	}

	if resourceCount != 3 {
		t.Errorf("gcp_cloud_resource facts = %d, want 3", resourceCount)
	}
	if webAttrs == nil {
		t.Fatalf("web-ip carried no attributes")
	}
	if webAttrs["is_external"] != true {
		t.Errorf("web-ip is_external = %v, want true", webAttrs["is_external"])
	}
	if webAttrs["address_type"] != "EXTERNAL" {
		t.Errorf("web-ip address_type = %v, want EXTERNAL", webAttrs["address_type"])
	}
	if webAttrs["user_count"] != 2 {
		t.Errorf("web-ip user_count = %v, want 2 (forwarding rule + instance)", webAttrs["user_count"])
	}
	// web-ip: 1 network, 1 forwarding-rule user, 1 instance user. internal-ip: 1
	// subnetwork. psc-ip (GlobalAddress): 1 network.
	if networkEdges != 2 {
		t.Errorf("address_in_network edges = %d, want 2 (web-ip + psc-ip global)", networkEdges)
	}
	if forwardingRuleEdges != 1 {
		t.Errorf("address_used_by_forwarding_rule edges = %d, want 1", forwardingRuleEdges)
	}
	if instanceEdges != 1 {
		t.Errorf("address_used_by_instance edges = %d, want 1", instanceEdges)
	}
	if subnetworkEdges != 1 {
		t.Errorf("address_in_subnetwork edges = %d, want 1", subnetworkEdges)
	}

	// No reserved IP address (external or internal) may reach a fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, forbidden := range []string{"203.0.113.50", "10.0.0.42", "203.0.113.99"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("envelope set leaked IP address %q", forbidden)
		}
	}
}
