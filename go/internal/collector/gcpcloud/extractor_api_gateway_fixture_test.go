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

// TestAPIGatewayOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for API Gateway Gateway through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the api_gateway_uses_api_config edge reach durable facts
// without any live GCP call, and that the raw defaultHostname DNS name never
// lands on a fact.
func TestAPIGatewayOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_api_gateway.json"))
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

	const prodGatewayFullName = "//apigateway.googleapis.com/projects/demo-project/locations/us-central1/gateways/prod-gw"

	resourceCount := 0
	relationshipCount := 0
	var prodAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == prodGatewayFullName {
				prodAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if env.Payload["relationship_type"] == relationshipTypeAPIGatewayUsesAPIConfig {
				relationshipCount++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if relationshipCount != 2 {
		t.Errorf("api_gateway_uses_api_config relationships = %d, want 2", relationshipCount)
	}
	if prodAttrs == nil {
		t.Fatalf("expected attributes for %s", prodGatewayFullName)
	}
	if prodAttrs["state"] != "ACTIVE" {
		t.Errorf("state = %v, want ACTIVE", prodAttrs["state"])
	}
	if prodAttrs["region"] != "us-central1" {
		t.Errorf("region = %v, want us-central1", prodAttrs["region"])
	}
	if _, ok := prodAttrs["default_hostname_fingerprint"]; !ok {
		t.Errorf("expected default_hostname_fingerprint to be present: %#v", prodAttrs)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"prod-gw-abc123.uc.gateway.dev", "prod-gw-abc123"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked raw defaultHostname token %q", token)
		}
	}
}
