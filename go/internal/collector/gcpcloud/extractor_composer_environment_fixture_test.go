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

// TestComposerEnvironmentOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for Composer Environment through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the typed-depth
// attributes, correlation anchors, and typed edges reach the durable facts
// without any live GCP call.
func TestComposerEnvironmentOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_composer_environment.json"))
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
			if env.Payload["full_resource_name"] == composerEnvironmentFullNameProdFixture {
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
		t.Fatalf("prod environment carried no attributes")
	}
	if prodAttrs["state"] != "RUNNING" {
		t.Errorf("prod state = %v, want RUNNING", prodAttrs["state"])
	}
	if prodAttrs["image_version"] != "composer-2.9.6-airflow-2.9.3" {
		t.Errorf("prod image_version = %v", prodAttrs["image_version"])
	}
	if len(prodAnchors) == 0 {
		t.Errorf("prod environment carried no correlation anchors")
	}

	// prod: GKE cluster, network, subnetwork, SA fingerprint, KMS key, DAG
	// bucket (1 each). staging: GKE cluster, DAG bucket (1 each; no network,
	// subnetwork, KMS key, or SA fingerprint since the default sentinel is
	// used and nodeConfig carries no network/subnetwork).
	if relTypes[relationshipTypeComposerEnvironmentUsesGKECluster] != 2 {
		t.Errorf("gke cluster edges = %d, want 2", relTypes[relationshipTypeComposerEnvironmentUsesGKECluster])
	}
	if relTypes[relationshipTypeComposerEnvironmentUsesDAGBucket] != 2 {
		t.Errorf("dag bucket edges = %d, want 2", relTypes[relationshipTypeComposerEnvironmentUsesDAGBucket])
	}
	if relTypes[relationshipTypeComposerEnvironmentUsesNetwork] != 1 {
		t.Errorf("network edges = %d, want 1", relTypes[relationshipTypeComposerEnvironmentUsesNetwork])
	}
	if relTypes[relationshipTypeComposerEnvironmentUsesSubnetwork] != 1 {
		t.Errorf("subnetwork edges = %d, want 1", relTypes[relationshipTypeComposerEnvironmentUsesSubnetwork])
	}
	if relTypes[relationshipTypeComposerEnvironmentEncryptedByKMSKey] != 1 {
		t.Errorf("kms key edges = %d, want 1", relTypes[relationshipTypeComposerEnvironmentEncryptedByKMSKey])
	}
}

const composerEnvironmentFullNameProdFixture = "//composer.googleapis.com/projects/demo-project/locations/us-central1/environments/prod"
