// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TagMatchStrength, AmbiguousMatchStrength, and RegistryProjectionBasis are
// the match_strength/identity_source values a resolved or ambiguous OCI
// registry-truth row carries. The digest-addressed match_strength
// (canonical_digest) moved to deployment.OciDigestMatchStrength with lane B2
// of #6060 (impact.canonicalOCIImageMatchCount is its only reader outside
// this package's truth builder).
const (
	TagMatchStrength        = "tag_resolved_to_digest"
	AmbiguousMatchStrength  = "ambiguous_tag"
	RegistryProjectionBasis = "oci_registry_projection"
)

// SplitImageRefs partitions imageRefs into digest-addressed refs (grouped by
// their parsed digest) and tag refs (deduplicated, sorted). A digest-
// addressed ref (repo@sha256:...) is recognized by ImageRefDigest; every
// other non-empty ref is treated as a mutable tag reference.
func SplitImageRefs(imageRefs []string) (map[string][]string, []string) {
	digestRefs := make(map[string][]string)
	tagRefs := make([]string, 0, len(imageRefs))
	seenTags := make(map[string]struct{}, len(imageRefs))
	for _, imageRef := range imageRefs {
		imageRef = strings.TrimSpace(imageRef)
		if imageRef == "" {
			continue
		}
		if digest := ImageRefDigest(imageRef); digest != "" {
			digestRefs[digest] = appendUniqueQueryString(digestRefs[digest], imageRef)
			continue
		}
		if _, exists := seenTags[imageRef]; exists {
			continue
		}
		seenTags[imageRef] = struct{}{}
		tagRefs = append(tagRefs, imageRef)
	}
	sort.Strings(tagRefs)
	return digestRefs, tagRefs
}

// ImageRefDigest returns the lowercased sha256 digest an image ref names
// (repo@sha256:<64 hex chars>), or "" when imageRef does not carry a
// syntactically valid digest.
func ImageRefDigest(imageRef string) string {
	_, digest, ok := strings.Cut(strings.TrimSpace(imageRef), "@")
	if !ok {
		return ""
	}
	digest = strings.ToLower(strings.TrimSpace(digest))
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return ""
	}
	for _, r := range strings.TrimPrefix(digest, "sha256:") {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return digest
}

// BuildDigestTruthRows shapes joined digest-addressed image rows (see
// JoinImageRepository) into truth rows, one per image ref that resolved to
// each row's digest.
func BuildDigestTruthRows(
	rows []map[string]any,
	digestRefs map[string][]string,
) []map[string]any {
	truth := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "digest")))
		if digest == "" {
			continue
		}
		for _, imageRef := range digestRefs[digest] {
			truth = append(truth, truthRow(row, imageRef, digest, deployment.OciDigestMatchStrength, "digest", false))
		}
	}
	return truth
}

// BuildTagTruthRows shapes joined tag-resolved rows (see
// JoinTagRepositoryImage) into truth rows: a tag ref whose observations
// resolved to exactly one digest becomes a resolved row; one that resolved
// to more than one digest becomes an ambiguous row carrying every candidate.
func BuildTagTruthRows(rows []map[string]any) []map[string]any {
	grouped := make(map[string][]map[string]any, len(rows))
	for _, row := range rows {
		imageRef := strings.TrimSpace(querycontract.StringVal(row, "image_ref"))
		digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "digest")))
		if imageRef == "" || digest == "" {
			continue
		}
		grouped[imageRef] = append(grouped[imageRef], row)
	}

	imageRefs := SortedMapKeys(grouped)
	truth := make([]map[string]any, 0, len(imageRefs))
	for _, imageRef := range imageRefs {
		group := grouped[imageRef]
		digests := uniqueSortedRowValues(group, "digest")
		if len(digests) != 1 {
			truth = append(truth, map[string]any{
				"image_ref":          imageRef,
				"match_strength":     AmbiguousMatchStrength,
				"truth_basis":        "observed_tag",
				"identity_strength":  "weak_tag",
				"identity_source":    RegistryProjectionBasis,
				"ambiguous":          true,
				"digest_candidates":  digests,
				"registry":           querycontract.StringVal(group[0], "registry"),
				"repository":         querycontract.StringVal(group[0], "repository"),
				"repository_id":      querycontract.StringVal(group[0], "repository_id"),
				"tag":                querycontract.StringVal(group[0], "tag"),
				"resolved_row_count": len(group),
			})
			continue
		}
		truth = append(truth, truthRow(group[0], imageRef, digests[0], TagMatchStrength, "tag_observation_with_digest", false))
	}
	return truth
}

func truthRow(
	row map[string]any,
	imageRef string,
	digest string,
	matchStrength string,
	truthBasis string,
	ambiguous bool,
) map[string]any {
	result := map[string]any{
		"image_ref":         imageRef,
		"digest":            digest,
		"image_id":          querycontract.StringVal(row, "image_id"),
		"registry":          querycontract.StringVal(row, "registry"),
		"repository":        querycontract.StringVal(row, "repository"),
		"repository_id":     querycontract.StringVal(row, "repository_id"),
		"media_type":        querycontract.StringVal(row, "media_type"),
		"provider":          querycontract.StringVal(row, "provider"),
		"match_strength":    matchStrength,
		"truth_basis":       truthBasis,
		"identity_source":   RegistryProjectionBasis,
		"identity_strength": "digest",
		"ambiguous":         ambiguous,
	}
	if tag := querycontract.StringVal(row, "tag"); tag != "" {
		result["tag"] = tag
		result["identity_strength"] = "tag_observation_with_digest"
	}
	return result
}

func uniqueSortedRowValues(rows []map[string]any, key string) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = appendUniqueQueryString(values, strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, key))))
	}
	sort.Strings(values)
	return values
}

// SortedMapKeys returns the sorted keys of values.
func SortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueQueryString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// IndexImagesByDigest keeps the first image row seen per digest so a tag can
// resolve its canonical image identity and media type. Digest is the
// canonical content address, so the first-wins policy is deterministic under
// an ordered per-label read sequence.
func IndexImagesByDigest(images []map[string]any) map[string]map[string]any {
	byDigest := make(map[string]map[string]any, len(images))
	for _, image := range images {
		digest := querycontract.StringVal(image, "digest")
		if digest == "" {
			continue
		}
		if _, exists := byDigest[digest]; !exists {
			byDigest[digest] = image
		}
	}
	return byDigest
}

// JoinImageRepository merges a digest-addressed image row with its registry
// repository into the row shape BuildDigestTruthRows consumes.
func JoinImageRepository(image, repo map[string]any) map[string]any {
	return map[string]any{
		"image_id":      querycontract.StringVal(image, "image_id"),
		"digest":        querycontract.StringVal(image, "digest"),
		"registry":      querycontract.StringVal(repo, "registry"),
		"repository":    querycontract.StringVal(repo, "repository"),
		"repository_id": querycontract.StringVal(image, "repository_id"),
		"media_type":    querycontract.StringVal(image, "media_type"),
		"provider":      querycontract.StringVal(repo, "provider"),
	}
}

// JoinTagRepositoryImage merges a tag observation with its registry
// repository and resolved image into the row shape BuildTagTruthRows
// consumes. The digest and repository_id come from the tag observation, the
// registry metadata from the repository, and the image identity/media type
// from the resolved image.
func JoinTagRepositoryImage(tag, repo, image map[string]any) map[string]any {
	return map[string]any{
		"image_ref":     querycontract.StringVal(tag, "image_ref"),
		"tag":           querycontract.StringVal(tag, "tag"),
		"digest":        querycontract.StringVal(tag, "digest"),
		"image_id":      querycontract.StringVal(image, "image_id"),
		"registry":      querycontract.StringVal(repo, "registry"),
		"repository":    querycontract.StringVal(repo, "repository"),
		"repository_id": querycontract.StringVal(tag, "repository_id"),
		"media_type":    querycontract.StringVal(image, "media_type"),
		"provider":      querycontract.StringVal(repo, "provider"),
	}
}
