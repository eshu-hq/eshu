// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

func init() {
	RegisterAssetExtractor(catalog.AssetTypeArtifactRegistryDockerImage, extractArtifactRegistryDockerImage)
}

// dockerImageData is the bounded view of a CAI
// artifactregistry.googleapis.com/DockerImage resource.data blob. Only
// redaction-safe control-plane metadata and the pullable image identity are
// decoded; the content digest is recovered from the URI (or the full resource
// name) rather than stored separately by the API. imageSizeBytes arrives as a
// JSON string in the Artifact Registry REST representation.
type dockerImageData struct {
	URI            string   `json:"uri"`
	Tags           []string `json:"tags"`
	ImageSizeBytes string   `json:"imageSizeBytes"`
	MediaType      string   `json:"mediaType"`
	BuildTime      string   `json:"buildTime"`
	UploadTime     string   `json:"uploadTime"`
}

// extractArtifactRegistryDockerImage extracts bounded typed depth for one
// Artifact Registry DockerImage CAI asset. It returns the Terraform/drift/
// monitoring attribute set (pullable URI, content digest, tags, image size,
// media type, build/upload time), the parent repository resource name and the
// content digest as cross-source correlation anchors (the digest is a
// content-addressable join key that image-identity correlation can key on), and
// the typed docker_image_in_repository edge. All captured fields are
// control-plane identifiers or metadata: no secret, no data layer content, and
// no raw response body leaves the extractor.
func extractArtifactRegistryDockerImage(ctx ExtractContext) (AttributeExtraction, error) {
	var data dockerImageData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode docker image data: %w", err)
	}

	digest := dockerImageDigest(data.URI, ctx.FullResourceName)
	attrs := dockerImageAttributes(data, digest)

	anchors := make([]string, 0, 2)
	rels := make([]RelationshipObservation, 0, 1)

	if repoName := dockerImageRepositoryFullName(ctx.FullResourceName); repoName != "" {
		anchors = append(anchors, repoName)
		rels = append(rels, dockerImageEdge(ctx, catalog.RelationshipTypeDockerImageInRepository, repoName, catalog.AssetTypeArtifactRegistryRepository))
	}
	if digest != "" {
		anchors = append(anchors, digest)
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dockerImageAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a "0 byte" image or an empty tag list.
func dockerImageAttributes(data dockerImageData, digest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.URI); v != "" {
		attrs["uri"] = v
	}
	if digest != "" {
		attrs["image_digest"] = digest
	}
	if tags := trimmedStrings(dedupeNonEmpty(data.Tags)); len(tags) > 0 {
		attrs["tags"] = tags
		attrs["tag_count"] = len(tags)
	}
	if v, ok := parseInt64String(data.ImageSizeBytes); ok {
		attrs["image_size_bytes"] = v
	}
	if v := strings.TrimSpace(data.MediaType); v != "" {
		attrs["media_type"] = v
	}
	if v, ok := normalizeRFC3339(data.BuildTime); ok {
		attrs["build_time"] = v
	}
	if v, ok := normalizeRFC3339(data.UploadTime); ok {
		attrs["upload_time"] = v
	}
	return attrs
}

// dockerImageDigest recovers the content digest (for example "sha256:abc123")
// from the pullable URI, falling back to the CAI full resource name when the URI
// is absent or unversioned. Both encode the digest after an "@" separator. It
// returns "" when neither carries a digest, so the attribute and anchor are
// omitted rather than fabricated.
func dockerImageDigest(uri, fullResourceName string) string {
	if digest := digestSuffix(uri); digest != "" {
		return digest
	}
	return digestSuffix(fullResourceName)
}

// digestSuffix returns the substring after the last "@" when it looks like a
// content digest reference, or "" otherwise.
func digestSuffix(ref string) string {
	trimmed := strings.TrimSpace(ref)
	idx := strings.LastIndex(trimmed, catalog.DigestSeparator)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[idx+len(catalog.DigestSeparator):])
}

// dockerImageRepositoryFullName derives the parent repository CAI full resource
// name from a DockerImage full resource name of the shape
// ".../repositories/<repo>/dockerImages/<image>@<digest>". It anchors on the
// repositories marker and keeps exactly the single repository segment, so a
// repository id of "dockerImages" or an image name that embeds "dockerImages/"
// does not truncate the parent name at the wrong "/dockerImages/". It returns ""
// when the name does not name an image under a repository, so the caller emits
// no parent-repository edge.
func dockerImageRepositoryFullName(fullResourceName string) string {
	trimmed := strings.TrimSpace(fullResourceName)
	idx := strings.Index(trimmed, catalog.RepositoriesPathMarker)
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+len(catalog.RepositoriesPathMarker):]
	repo, after, found := strings.Cut(rest, "/")
	if !found || strings.TrimSpace(repo) == "" {
		return ""
	}
	if !strings.HasPrefix(after, catalog.DockerImagesPathPrefix) {
		return ""
	}
	return trimmed[:idx] + catalog.RepositoriesPathMarker + repo
}

func dockerImageEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
