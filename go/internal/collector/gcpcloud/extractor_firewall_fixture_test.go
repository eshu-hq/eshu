// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestFirewallOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for compute Firewall through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the network edge reach durable facts without any live
// GCP call, and that no source/destination IP range leaks.
func TestFirewallOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_firewall.json"))
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
	var allowAttrs map[string]any
	const allowWebFullName = "//compute.googleapis.com/projects/demo-project/global/firewalls/allow-web"
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == allowWebFullName {
				allowAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeFirewallAppliesToNetwork {
				networkEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if allowAttrs == nil {
		t.Fatalf("allow-web carried no attributes")
	}
	if allowAttrs["direction"] != "INGRESS" {
		t.Errorf("allow-web direction = %v, want INGRESS", allowAttrs["direction"])
	}
	if allowAttrs["opens_to_public"] != true {
		t.Errorf("allow-web opens_to_public = %v, want true", allowAttrs["opens_to_public"])
	}
	// Both firewalls reference the same network -> 2 edges.
	if networkEdges != 2 {
		t.Errorf("firewall_applies_to_network edges = %d, want 2", networkEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	// No source/destination IP range, and no raw target service-account email,
	// may reach a fact; the SA email survives only as a fingerprinted digest.
	const targetSAEmail = "runtime@demo-project.iam.gserviceaccount.com"
	for _, forbidden := range []string{"0.0.0.0/0", "10.0.0.0/8", targetSAEmail, "runtime@demo-project"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("envelope set leaked forbidden value %q", forbidden)
		}
	}
	saDigest := secretsiam.GCPServiceAccountEmailDigest(targetSAEmail)
	if saDigest == "" || !containsString(string(blob), saDigest) {
		t.Fatalf("expected the fingerprinted target service-account digest %q to be persisted as an anchor", saDigest)
	}
}
