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

// TestDataplexEntryGroupOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Dataplex Entry Group through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes reach durable facts without any live GCP call and that the
// container asset produces no edges.
func TestDataplexEntryGroupOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_dataplex_entry_group.json"))
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
	relationshipCount := 0
	var analyticsAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == dataplexEntryGroupFullName {
				analyticsAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			relationshipCount++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if relationshipCount != 0 {
		t.Errorf("entry group is a container and must emit no relationship facts, got %d", relationshipCount)
	}
	if analyticsAttrs == nil {
		t.Fatalf("analytics entry group carried no attributes")
	}
	if analyticsAttrs["state"] != "ACTIVE" {
		t.Errorf("analytics state = %v, want ACTIVE", analyticsAttrs["state"])
	}
	if analyticsAttrs["transfer_status"] != "TRANSFER_STATUS_MIGRATED" {
		t.Errorf("analytics transfer_status = %v, want TRANSFER_STATUS_MIGRATED", analyticsAttrs["transfer_status"])
	}
	if analyticsAttrs["creation_time"] != "2024-05-01T00:00:00Z" {
		t.Errorf("analytics creation_time = %v, want 2024-05-01T00:00:00Z", analyticsAttrs["creation_time"])
	}
}
