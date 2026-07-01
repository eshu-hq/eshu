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

// TestDockerImageOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Artifact Registry DockerImage through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes (uri, digest, tags, size, media type), the digest correlation
// anchor, and the parent-repository edge reach durable facts without any live GCP
// call, and that the raw resource.data blob never becomes a fact field.
func TestDockerImageOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_artifact_registry_docker_image.json"))
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
	repoEdges := 0
	var apiAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == dockerImageFullName {
				apiAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeDockerImageInRepository {
				repoEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if apiAttrs == nil {
		t.Fatalf("api docker image carried no attributes")
	}
	if apiAttrs["image_digest"] != "sha256:abc123" {
		t.Errorf("image_digest = %v, want sha256:abc123", apiAttrs["image_digest"])
	}
	if apiAttrs["media_type"] != "application/vnd.docker.distribution.manifest.v2+json" {
		t.Errorf("media_type = %v, want docker manifest v2", apiAttrs["media_type"])
	}
	if apiAttrs["image_size_bytes"] == nil {
		t.Errorf("api docker image carried no image_size_bytes")
	}
	// Both images live in the same repository.
	if repoEdges != 2 {
		t.Errorf("docker_image_in_repository edges = %d, want 2", repoEdges)
	}

	// The parsed facts must carry the intended identity (digest) but never a raw
	// resource.data field name: the extractor selects a bounded snake_case
	// attribute set (image_size_bytes, media_type, build_time, upload_time) and
	// drops resource.data's own updateTime, so none of the camelCase
	// resource.data keys may echo into any durable fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if !containsString(string(blob), "sha256:abc123") {
		t.Fatalf("expected the content digest to reach durable facts")
	}
	for _, rawKey := range []string{"imageSizeBytes", "mediaType", "buildTime", "uploadTime", "updateTime"} {
		if containsString(string(blob), rawKey) {
			t.Fatalf("raw resource.data field %q leaked into durable facts: %s", rawKey, blob)
		}
	}
}
