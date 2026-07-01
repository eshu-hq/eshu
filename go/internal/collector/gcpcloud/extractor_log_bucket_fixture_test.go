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

// TestLogBucketOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Logging Log Bucket through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes and
// the CMEK edge reach durable facts without any live GCP call.
func TestLogBucketOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_log_bucket.json"))
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
	kmsEdges := 0
	var defaultAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == logBucketFullName {
				defaultAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeLogBucketEncryptedByKMSKey {
				kmsEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if defaultAttrs == nil || defaultAttrs["locked"] != true {
		t.Errorf("_Default bucket attrs missing/incorrect: %#v", defaultAttrs)
	}
	if defaultAttrs["customer_managed_encryption"] != true {
		t.Errorf("_Default bucket customer_managed_encryption = %v, want true", defaultAttrs["customer_managed_encryption"])
	}
	if kmsEdges != 1 {
		t.Errorf("log_bucket_encrypted_by_kms_key edges = %d, want 1", kmsEdges)
	}
}
