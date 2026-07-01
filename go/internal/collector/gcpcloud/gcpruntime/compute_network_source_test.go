// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsComputeNetworkTypedDepthFromFixture proves the collector runtime
// drains the offline VPC Network CAI fixture through the credential-free fixture
// provider and emits a gcp_cloud_resource fact carrying the bounded typed-depth
// attributes plus typed subnetwork and peering relationship facts — the
// replayable-in-CI path with no live GCP call.
func TestSourceEmitsComputeNetworkTypedDepthFromFixture(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {readFixturePage(t, "assets_list_compute_network.json")},
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
	// 2 subnetwork edges + 1 peering edge.
	if got := countKind(envs, facts.GCPCloudRelationshipFactKind); got != 3 {
		t.Fatalf("relationship fact count = %d, want 3 (2 subnets, 1 peering)", got)
	}

	resource := firstEnvelopeKind(t, envs, facts.GCPCloudResourceFactKind)
	attrs, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource fact carried no attributes map: %#v", resource.Payload["attributes"])
	}
	if attrs["routing_mode"] != "GLOBAL" {
		t.Errorf("attributes.routing_mode = %v, want GLOBAL", attrs["routing_mode"])
	}
	if attrs["auto_create_subnetworks"] != false {
		t.Errorf("attributes.auto_create_subnetworks = %v, want false", attrs["auto_create_subnetworks"])
	}
	anchors, ok := resource.Payload["correlation_anchors"].([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("resource fact carried no correlation anchors: %#v", resource.Payload["correlation_anchors"])
	}
}
