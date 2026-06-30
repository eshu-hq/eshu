// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsBigQueryTableTypedDepthFromFixture proves the collector runtime
// drains the offline BigQuery Table CAI fixture through the credential-free
// fixture provider and emits a gcp_cloud_resource fact carrying the bounded
// typed-depth attributes plus typed parent-Dataset and KMS relationship facts —
// the replayable-in-CI path with no live GCP call.
func TestSourceEmitsBigQueryTableTypedDepthFromFixture(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {readFixturePage(t, "assets_list_bigquery_table.json")},
	})
	src := newSource(t, testConfig(testScope()), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}

	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	// Parent Dataset + KMS-key typed edges.
	if got := countKind(envs, facts.GCPCloudRelationshipFactKind); got != 2 {
		t.Fatalf("relationship fact count = %d, want 2 (dataset, kms)", got)
	}

	resource := firstEnvelopeKind(t, envs, facts.GCPCloudResourceFactKind)
	attrs, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource fact carried no attributes map: %#v", resource.Payload["attributes"])
	}
	if attrs["table_type"] != "TABLE" {
		t.Errorf("attributes.table_type = %v, want TABLE", attrs["table_type"])
	}
	if attrs["kms_key_name"] != "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1" {
		t.Errorf("attributes.kms_key_name = %v", attrs["kms_key_name"])
	}
	anchors, ok := resource.Payload["correlation_anchors"].([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("resource fact carried no correlation anchors: %#v", resource.Payload["correlation_anchors"])
	}
}
