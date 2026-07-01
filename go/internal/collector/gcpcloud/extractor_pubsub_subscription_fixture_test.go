// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestPubSubSubscriptionOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Pub/Sub Subscription through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the topic / dead-letter / BigQuery / Cloud Storage edges reach
// durable facts without any live GCP call, and that no raw push endpoint host,
// path, or token ever lands on a fact.
func TestPubSubSubscriptionOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_pubsub_subscription.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(page.Resources))
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
	var ordersAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == pubSubSubscriptionFullName {
				ordersAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			edges[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 3 {
		t.Errorf("gcp_cloud_resource facts = %d, want 3", resourceCount)
	}
	if ordersAttrs == nil {
		t.Fatalf("orders-worker subscription carried no attributes")
	}
	if ordersAttrs["delivery_type"] != "push" {
		t.Errorf("orders-worker delivery_type = %v, want push", ordersAttrs["delivery_type"])
	}
	if ordersAttrs["push_endpoint_scheme"] != "https" {
		t.Errorf("orders-worker push_endpoint_scheme = %v, want https", ordersAttrs["push_endpoint_scheme"])
	}
	// The host must be present as a fingerprint (not raw), proving the positive
	// extraction path alongside the negative leak assertion below.
	if fp, ok := ordersAttrs["push_endpoint_host_fingerprint"].(string); !ok || !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("orders-worker push_endpoint_host_fingerprint = %v, want a sha256: digest", ordersAttrs["push_endpoint_host_fingerprint"])
	}
	for rel, want := range map[string]int{
		relationshipTypeSubscriptionSubscribesToTopic:      3,
		relationshipTypeSubscriptionDeadLettersToTopic:     1,
		relationshipTypeSubscriptionExportsToStorageBucket: 1,
		relationshipTypeSubscriptionExportsToBigQueryTable: 1,
	} {
		if edges[rel] != want {
			t.Errorf("%s edges = %d, want %d", rel, edges[rel], want)
		}
	}

	// No raw push endpoint host, path, or token may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"ingest.example.com", "_ah/push", "PLACEHOLDER", "https://ingest"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked push-endpoint token %q", token)
		}
	}
}
