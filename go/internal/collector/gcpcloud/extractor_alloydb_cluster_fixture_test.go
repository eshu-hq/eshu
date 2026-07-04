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

// TestAlloyDBClusterOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for AlloyDB Cluster through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the typed-depth attributes,
// correlation anchors, and the network/KMS edges reach the durable facts
// without any live GCP call.
func TestAlloyDBClusterOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_alloydb_cluster.json"))
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
	var primaryAttrs map[string]any
	var primaryAnchors []any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == "//alloydb.googleapis.com/projects/demo-project/locations/us-central1/clusters/primary" {
				primaryAttrs, _ = env.Payload["attributes"].(map[string]any)
				primaryAnchors, _ = env.Payload["correlation_anchors"].([]any)
				if primaryAnchors == nil {
					if s, ok := env.Payload["correlation_anchors"].([]string); ok {
						for _, v := range s {
							primaryAnchors = append(primaryAnchors, v)
						}
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
	if primaryAttrs == nil {
		t.Fatalf("primary cluster carried no attributes")
	}
	if primaryAttrs["state"] != "READY" {
		t.Errorf("primary state = %v, want READY", primaryAttrs["state"])
	}
	if primaryAttrs["cluster_type"] != "PRIMARY" {
		t.Errorf("primary cluster_type = %v, want PRIMARY", primaryAttrs["cluster_type"])
	}
	if primaryAttrs["kms_key_name"] != "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key" {
		t.Errorf("primary kms_key_name = %v", primaryAttrs["kms_key_name"])
	}
	if len(primaryAnchors) == 0 {
		t.Errorf("primary cluster carried no correlation anchors")
	}

	// Only the primary cluster has network/KMS config; the secondary fixture
	// entry has neither, so it emits no edges.
	if relTypes[relationshipTypeAlloyDBClusterInNetwork] != 1 {
		t.Errorf("network edges = %d, want 1", relTypes[relationshipTypeAlloyDBClusterInNetwork])
	}
	if relTypes[relationshipTypeAlloyDBClusterEncryptedByKMSKey] != 1 {
		t.Errorf("kms edges = %d, want 1", relTypes[relationshipTypeAlloyDBClusterEncryptedByKMSKey])
	}
}
