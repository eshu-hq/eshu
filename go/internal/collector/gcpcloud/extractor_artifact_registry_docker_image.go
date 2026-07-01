// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the Artifact Registry DockerImage typed-depth
// extractor and the relationship endpoint it derives. The target asset type
// names the CAI asset type of the parent-repository edge so the reducer can
// resolve both endpoints exactly before materializing.
const (
	assetTypeArtifactRegistryDockerImage = "artifactregistry.googleapis.com/DockerImage"
	assetTypeArtifactRegistryRepository  = "artifactregistry.googleapis.com/Repository"
)

// relationshipTypeDockerImageInRepository is the bounded provider relationship
// type for the image -> parent repository edge carried on a gcp_cloud_relationship
// fact. The reducer materializes the edge only when both endpoints resolve
// exactly. Build (built-by) and Run/GKE (deployed-to) edges are intentionally not
// emitted here: they are cross-source correlations keyed on the content digest
// from the deploying resource's own image references, not fields of this asset's
// resource.data, so this extractor never fabricates an unresolved endpoint.
const relationshipTypeDockerImageInRepository = "docker_image_in_repository"

// repositoriesPathMarker and dockerImagesPathPrefix locate the parent repository
// inside a CAI DockerImage full resource name of the shape
// ".../repositories/<repo>/dockerImages/<image>@<digest>". The repository is the
// single segment after the repositories marker; anchoring on it (rather than the
// first "/dockerImages/" match) keeps the parse correct when the repository id is
// itself "dockerImages" or the image name embeds that token.
const (
	repositoriesPathMarker = "/repositories/"
	dockerImagesPathPrefix = "dockerImages/"
)

// digestSeparator separates an image path from its content digest in both the
// pullable URI and the CAI full resource name.
const digestSeparator = "@"

func init() {
	RegisterAssetExtractor(assetTypeArtifactRegistryDockerImage, extractArtifactRegistryDockerImage)
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
		rels = append(rels, dockerImageEdge(ctx, relationshipTypeDockerImageInRepository, repoName, assetTypeArtifactRegistryRepository))
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
	idx := strings.LastIndex(trimmed, digestSeparator)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[idx+len(digestSeparator):])
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
	idx := strings.Index(trimmed, repositoriesPathMarker)
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+len(repositoriesPathMarker):]
	repo, after, found := strings.Cut(rest, "/")
	if !found || strings.TrimSpace(repo) == "" {
		return ""
	}
	if !strings.HasPrefix(after, dockerImagesPathPrefix) {
		return ""
	}
	return trimmed[:idx] + repositoriesPathMarker + repo
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
