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

// TestDiskOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// compute Disk through parse -> normalize -> attribute extraction -> generation
// -> envelope, proving the redaction-safe typed-depth attributes, correlation
// anchors, and the instance/image/snapshot/KMS edges reach durable facts without
// any live GCP call, and that no KMS key material or raw disk data leaks.
func TestDiskOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_disk.json"))
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
	instanceEdges := 0
	imageEdges := 0
	snapshotEdges := 0
	kmsEdges := 0
	var dataAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == diskFullName {
				dataAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeDiskAttachedToInstance:
				instanceEdges++
			case relationshipTypeDiskCreatedFromImage:
				imageEdges++
			case relationshipTypeDiskCreatedFromSnapshot:
				snapshotEdges++
			case relationshipTypeDiskEncryptedByKey:
				kmsEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if dataAttrs == nil {
		t.Fatalf("data-disk carried no attributes")
	}
	if dataAttrs["status"] != "READY" {
		t.Errorf("data-disk status = %v, want READY", dataAttrs["status"])
	}
	if dataAttrs["disk_type"] != "pd-ssd" {
		t.Errorf("data-disk disk_type = %v, want pd-ssd", dataAttrs["disk_type"])
	}
	// data-disk: 1 instance, 1 image, 1 kms. boot-disk: 1 snapshot.
	if instanceEdges != 1 {
		t.Errorf("disk_attached_to_instance edges = %d, want 1", instanceEdges)
	}
	if imageEdges != 1 {
		t.Errorf("disk_created_from_image edges = %d, want 1", imageEdges)
	}
	if snapshotEdges != 1 {
		t.Errorf("disk_created_from_snapshot edges = %d, want 1", snapshotEdges)
	}
	if kmsEdges != 1 {
		t.Errorf("disk_encrypted_by_key edges = %d, want 1", kmsEdges)
	}

	// The raw KMS key version suffix must never reach a fact; the extractor keeps
	// only the bounded CryptoKey resource name (edge target + anchor). Bounded
	// resource labels legitimately ride the base gcp_cloud_resource observation,
	// so they are not asserted absent here.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if containsString(string(blob), "cryptoKeyVersions/3") {
		t.Fatalf("envelope set leaked KMS key version suffix")
	}
}
