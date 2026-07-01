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

// TestDataprocClusterOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Dataproc Cluster through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes
// and the network/subnetwork/CMEK/staging-bucket edges reach durable facts
// without any live GCP call, and that no raw service-account email lands on a
// fact.
func TestDataprocClusterOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_dataproc_cluster.json"))
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
	edges := map[string]int{}
	var analyticsAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == dataprocClusterFullName {
				analyticsAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			edges[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if analyticsAttrs == nil {
		t.Fatalf("analytics cluster carried no attributes")
	}
	if analyticsAttrs["status_state"] != "RUNNING" {
		t.Errorf("analytics status_state = %v, want RUNNING", analyticsAttrs["status_state"])
	}
	if analyticsAttrs["customer_managed_encryption"] != true {
		t.Errorf("analytics customer_managed_encryption = %v, want true", analyticsAttrs["customer_managed_encryption"])
	}
	if analyticsAttrs["autoscaling_enabled"] != true {
		t.Errorf("analytics autoscaling_enabled = %v, want true", analyticsAttrs["autoscaling_enabled"])
	}
	for rel, want := range map[string]int{
		relationshipTypeClusterUsesNetwork:       1,
		relationshipTypeClusterUsesSubnetwork:    1,
		relationshipTypeClusterEncryptedByKMSKey: 1,
		relationshipTypeClusterUsesStagingBucket: 1,
	} {
		if edges[rel] != want {
			t.Errorf("%s edges = %d, want %d", rel, edges[rel], want)
		}
	}

	// No raw service-account email may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if containsString(string(blob), "dataproc-runner@demo-project.iam.gserviceaccount.com") {
		t.Fatalf("envelope set leaked raw service-account email")
	}
}
