// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const dockerImageFullName = "//artifactregistry.googleapis.com/projects/demo-project/locations/us/repositories/app-images/dockerImages/api@sha256:abc123"

func dockerImageContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dockerImageFullName,
		AssetType:        assetTypeArtifactRegistryDockerImage,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDockerImageExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeArtifactRegistryDockerImage); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeArtifactRegistryDockerImage)
	}
}

func TestExtractDockerImageFullResource(t *testing.T) {
	const data = `{
		"uri": "us-docker.pkg.dev/demo-project/app-images/api@sha256:abc123",
		"tags": ["latest", "v1.2.3", "latest"],
		"imageSizeBytes": "12345678",
		"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
		"buildTime": "2024-06-01T00:00:00Z",
		"uploadTime": "2024-06-01T01:00:00.500Z",
		"updateTime": "2024-06-01T02:00:00Z"
	}`

	got, err := extractArtifactRegistryDockerImage(dockerImageContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"uri":              "us-docker.pkg.dev/demo-project/app-images/api@sha256:abc123",
		"image_digest":     "sha256:abc123",
		"tags":             []string{"latest", "v1.2.3"},
		"tag_count":        2,
		"image_size_bytes": int64(12345678),
		"media_type":       "application/vnd.docker.distribution.manifest.v2+json",
		"build_time":       "2024-06-01T00:00:00Z",
		"upload_time":      "2024-06-01T01:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// Parent repository resource name and the content digest are the cross-source
	// join keys: the repository anchors the artifact registry parent, the digest
	// anchors container_image_identity correlation.
	wantAnchors := []string{
		"//artifactregistry.googleapis.com/projects/demo-project/locations/us/repositories/app-images",
		"sha256:abc123",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the parent-repository edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDockerImageInRepository,
		"//artifactregistry.googleapis.com/projects/demo-project/locations/us/repositories/app-images",
		assetTypeArtifactRegistryRepository)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != dockerImageFullName {
		t.Errorf("relationship source = %q, want docker image full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != assetTypeArtifactRegistryDockerImage {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeArtifactRegistryDockerImage)
	}
}

func TestExtractDockerImageDigestFromNameFallback(t *testing.T) {
	// No uri field: the content digest is still recoverable from the CAI full
	// resource name, which encodes it after the image path.
	got, err := extractArtifactRegistryDockerImage(dockerImageContext(`{"tags": ["v1"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["image_digest"] != "sha256:abc123" {
		t.Errorf("image_digest = %v, want sha256:abc123 (from full resource name)", got.Attributes["image_digest"])
	}
	if _, ok := got.Attributes["uri"]; ok {
		t.Errorf("uri must be omitted when absent, got %#v", got.Attributes)
	}
	// The digest anchor is still present even without a uri.
	assertAnchor(t, got.CorrelationAnchors, "sha256:abc123")
}

func TestExtractDockerImagePartialData(t *testing.T) {
	// Only uri present: digest + uri resolve, the parent repository edge resolves
	// from the full name, and unreported fields are omitted rather than zeroed.
	const data = `{"uri": "us-docker.pkg.dev/demo-project/app-images/api@sha256:abc123"}`
	got, err := extractArtifactRegistryDockerImage(dockerImageContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"uri":          "us-docker.pkg.dev/demo-project/app-images/api@sha256:abc123",
		"image_digest": "sha256:abc123",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected the parent-repository edge, got %#v", got.Relationships)
	}
}

func TestExtractDockerImageEmptyDataDerivesFromName(t *testing.T) {
	// Empty resource.data still carries the digest and the parent repository
	// because the CAI full resource name encodes both.
	got, err := extractArtifactRegistryDockerImage(dockerImageContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["image_digest"] != "sha256:abc123" {
		t.Errorf("image_digest = %v, want sha256:abc123", got.Attributes["image_digest"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the derived repository edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDockerImageInRepository,
		"//artifactregistry.googleapis.com/projects/demo-project/locations/us/repositories/app-images",
		assetTypeArtifactRegistryRepository)
}

func TestExtractDockerImageMalformedDataErrors(t *testing.T) {
	if _, err := extractArtifactRegistryDockerImage(dockerImageContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestDockerImageRepositoryFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"full docker image name",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/r/dockerImages/img@sha256:d",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/r",
		},
		{
			"no dockerImages segment",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/r",
			"",
		},
		{
			// Repository id is literally "dockerImages": the parse must keep the
			// repository segment, not truncate at the earlier "/dockerImages/".
			"repository id is dockerImages",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/dockerImages/dockerImages/img@sha256:d",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/dockerImages",
		},
		{
			// Image name embeds "dockerImages/": only the true repository segment
			// after /repositories/ is kept.
			"image name embeds dockerImages",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/r/dockerImages/nested/dockerImages/img@sha256:d",
			"//artifactregistry.googleapis.com/projects/p/locations/us/repositories/r",
		},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerImageRepositoryFullName(tc.in); got != tc.want {
				t.Errorf("dockerImageRepositoryFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDockerImageDigest(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		full string
		want string
	}{
		{"from uri", "host/p/r/img@sha256:deadbeef", "irrelevant", "sha256:deadbeef"},
		{"from full name when uri blank", "", "//x/dockerImages/img@sha256:cafe", "sha256:cafe"},
		{"uri without digest falls back to name", "host/p/r/img", "//x/dockerImages/img@sha256:beef", "sha256:beef"},
		{"neither carries a digest", "host/p/r/img", "//x/dockerImages/img", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerImageDigest(tc.uri, tc.full); got != tc.want {
				t.Errorf("dockerImageDigest(%q,%q) = %q, want %q", tc.uri, tc.full, got, tc.want)
			}
		})
	}
}

func assertAnchor(t *testing.T, anchors []string, want string) {
	t.Helper()
	for _, a := range anchors {
		if a == want {
			return
		}
	}
	t.Errorf("missing anchor %q in %#v", want, anchors)
}
