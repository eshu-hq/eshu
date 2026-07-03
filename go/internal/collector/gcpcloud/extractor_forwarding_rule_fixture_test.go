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

// TestForwardingRuleOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for compute ForwardingRule through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes, correlation anchors, and target/network/subnetwork edges reach
// durable facts without any live GCP call, and that no IP address ever lands
// on a fact.
func TestForwardingRuleOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_forwarding_rule.json"))
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
	relTypeCounts := map[string]int{}
	var frontendAttrs map[string]any
	var internalAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			switch env.Payload["full_resource_name"] {
			case "//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/frontend-lb":
				frontendAttrs, _ = env.Payload["attributes"].(map[string]any)
			case "//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/internal-lb":
				internalAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			relTypeCounts[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if frontendAttrs == nil {
		t.Fatalf("frontend-lb carried no attributes")
	}
	if frontendAttrs["load_balancing_scheme"] != "EXTERNAL" {
		t.Errorf("frontend-lb load_balancing_scheme = %v, want EXTERNAL", frontendAttrs["load_balancing_scheme"])
	}
	if frontendAttrs["is_external"] != true {
		t.Errorf("frontend-lb is_external = %v, want true", frontendAttrs["is_external"])
	}
	if internalAttrs == nil {
		t.Fatalf("internal-lb carried no attributes")
	}
	if internalAttrs["is_external"] != false {
		t.Errorf("internal-lb is_external = %v, want false", internalAttrs["is_external"])
	}

	if relTypeCounts[relationshipTypeForwardingRuleTargetsTargetPool] != 1 {
		t.Errorf("target-pool edges = %d, want 1", relTypeCounts[relationshipTypeForwardingRuleTargetsTargetPool])
	}
	if relTypeCounts[relationshipTypeForwardingRuleTargetsBackendService] != 1 {
		t.Errorf("backend-service edges = %d, want 1", relTypeCounts[relationshipTypeForwardingRuleTargetsBackendService])
	}
	if relTypeCounts[relationshipTypeForwardingRuleInNetwork] != 2 {
		t.Errorf("network edges = %d, want 2", relTypeCounts[relationshipTypeForwardingRuleInNetwork])
	}
	if relTypeCounts[relationshipTypeForwardingRuleInSubnetwork] != 2 {
		t.Errorf("subnetwork edges = %d, want 2", relTypeCounts[relationshipTypeForwardingRuleInSubnetwork])
	}

	// No IP address token from the fixture may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"203.0.113.10", "10.0.0.20"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked IP token %q", token)
		}
	}
}
