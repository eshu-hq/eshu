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

// TestArtifactRegistryRepositoryOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for Artifact Registry Repository through parse -> normalize
// -> attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes, the CMEK correlation anchor, and the CMEK edge reach
// durable facts without any live GCP call.
func TestArtifactRegistryRepositoryOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_artifact_registry_repository.json"))
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
	var dockerAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == artifactRepoFullName {
				dockerAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeArtifactRepoEncryptedByKMSKey {
				kmsEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if dockerAttrs == nil {
		t.Fatalf("docker repository carried no attributes")
	}
	if dockerAttrs["format"] != "DOCKER" {
		t.Errorf("format = %v, want DOCKER", dockerAttrs["format"])
	}
	if dockerAttrs["cleanup_policy_count"] != float64(2) && dockerAttrs["cleanup_policy_count"] != 2 {
		t.Errorf("cleanup_policy_count = %v, want 2", dockerAttrs["cleanup_policy_count"])
	}
	if kmsEdges != 1 {
		t.Errorf("artifact_registry_repository_encrypted_by_kms_key edges = %d, want 1", kmsEdges)
	}
}
