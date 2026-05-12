package ociruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

type parsedManifest struct {
	MediaType string
	Config    ociregistry.Descriptor
	Layers    []ociregistry.Descriptor
	Manifests []ociregistry.Descriptor
}

func parseManifest(response distribution.ManifestResponse) (string, parsedManifest, error) {
	var raw struct {
		MediaType string          `json:"mediaType"`
		Config    rawDescriptor   `json:"config"`
		Layers    []rawDescriptor `json:"layers"`
		Manifests []rawDescriptor `json:"manifests"`
	}
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &raw); err != nil {
			return "", parsedManifest{}, err
		}
	}
	mediaType := cleanMediaType(response.MediaType)
	if mediaType == "" {
		mediaType = strings.TrimSpace(raw.MediaType)
	}
	parsed := parsedManifest{
		MediaType: mediaType,
		Config:    raw.Config.descriptor(),
		Layers:    rawDescriptors(raw.Layers),
		Manifests: rawDescriptors(raw.Manifests),
	}
	return mediaType, parsed, nil
}

func manifestDigest(response distribution.ManifestResponse) (string, string, bool) {
	digest := strings.TrimSpace(response.Digest)
	if digest != "" {
		return digest, "", true
	}
	if len(response.Body) == 0 {
		return "", "", false
	}
	sum := sha256.Sum256(response.Body)
	return "sha256:" + hex.EncodeToString(sum[:]), ociregistry.WarningComputedManifestDigest, true
}

type rawDescriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	ArtifactType string            `json:"artifactType"`
	Annotations  map[string]string `json:"annotations"`
	Platform     struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}

func (d rawDescriptor) descriptor() ociregistry.Descriptor {
	return ociregistry.Descriptor{
		Digest:       d.Digest,
		MediaType:    d.MediaType,
		SizeBytes:    d.Size,
		ArtifactType: d.ArtifactType,
		Annotations:  d.Annotations,
		Platform: ociregistry.Platform{
			OS:           d.Platform.OS,
			Architecture: d.Platform.Architecture,
			Variant:      d.Platform.Variant,
		},
	}
}

func rawDescriptors(raw []rawDescriptor) []ociregistry.Descriptor {
	descriptors := make([]ociregistry.Descriptor, 0, len(raw))
	for _, descriptor := range raw {
		descriptors = append(descriptors, descriptor.descriptor())
	}
	return descriptors
}

func cleanMediaType(raw string) string {
	before, _, _ := strings.Cut(strings.TrimSpace(raw), ";")
	return strings.TrimSpace(before)
}

func mediaFamily(mediaType string) string {
	switch cleanMediaType(mediaType) {
	case ociregistry.MediaTypeOCIImageIndex, ociregistry.MediaTypeDockerManifestList:
		return "image_index"
	case ociregistry.MediaTypeOCIImageManifest, ociregistry.MediaTypeDockerImageManifest:
		return "image_manifest"
	default:
		return "descriptor"
	}
}

func artifactFamily(artifactType string) string {
	trimmed := strings.ToLower(strings.TrimSpace(artifactType))
	if trimmed == "" {
		return "unknown"
	}
	switch {
	case strings.Contains(trimmed, "sbom"), strings.Contains(trimmed, "spdx"), strings.Contains(trimmed, "cyclonedx"):
		return "sbom"
	case strings.Contains(trimmed, "signature"), strings.Contains(trimmed, "sigstore"), strings.Contains(trimmed, "cosign"):
		return "signature"
	case strings.Contains(trimmed, "attestation"), strings.Contains(trimmed, "in-toto"), strings.Contains(trimmed, "slsa"):
		return "attestation"
	case strings.Contains(trimmed, "vulnerability"), strings.Contains(trimmed, "scan"):
		return "vulnerability"
	default:
		return "other"
	}
}
