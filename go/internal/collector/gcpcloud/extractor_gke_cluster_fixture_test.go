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

// TestGKEClusterOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for GKE Cluster through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the typed-depth attributes, correlation
// anchors, and typed edges reach the durable facts without any live GCP call.
func TestGKEClusterOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_gke_cluster.json"))
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
			if env.Payload["full_resource_name"] == gkeClusterFullNameProdFixture {
				prodAttrs, _ = env.Payload["attributes"].(map[string]any)
				prodAnchors = anyStringSlice(env.Payload["correlation_anchors"])
			}
		case facts.GCPCloudRelationshipFactKind:
			relTypes[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if prodAttrs == nil {
		t.Fatalf("prod cluster carried no attributes")
	}
	if prodAttrs["status"] != "RUNNING" {
		t.Errorf("prod status = %v, want RUNNING", prodAttrs["status"])
	}
	if prodAttrs["current_master_version"] != "1.29.1-gke.1589000" {
		t.Errorf("prod current_master_version = %v", prodAttrs["current_master_version"])
	}
	if len(prodAnchors) == 0 {
		t.Errorf("prod cluster carried no correlation anchors")
	}

	// prod: network + subnetwork (2). staging: network + subnetwork (2, resolved
	// from the bare "default" short names).
	if relTypes[relationshipTypeGKEClusterUsesNetwork] != 2 {
		t.Errorf("network edges = %d, want 2", relTypes[relationshipTypeGKEClusterUsesNetwork])
	}
	if relTypes[relationshipTypeGKEClusterUsesSubnetwork] != 2 {
		t.Errorf("subnetwork edges = %d, want 2", relTypes[relationshipTypeGKEClusterUsesSubnetwork])
	}
}

const gkeClusterFullNameProdFixture = "//container.googleapis.com/projects/demo-project/locations/us-central1/clusters/prod"

func anyStringSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	if s, ok := v.([]string); ok {
		out := make([]any, 0, len(s))
		for _, item := range s {
			out = append(out, item)
		}
		return out
	}
	return nil
}
