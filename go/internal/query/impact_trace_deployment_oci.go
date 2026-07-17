// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	ociDigestMatchStrength     = "canonical_digest"
	ociTagMatchStrength        = "tag_resolved_to_digest"
	ociAmbiguousMatchStrength  = "ambiguous_tag"
	ociRegistryProjectionBasis = "oci_registry_projection"
)

var ociImageLookupLabels = []string{
	"ContainerImage",
	"ContainerImageIndex",
	"ContainerImageDescriptor",
}

func (h *ImpactHandler) fetchOCIImageRegistryTruth(
	ctx context.Context,
	imageRefs []string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	return fetchOCIImageRegistryTruth(ctx, h.Neo4j, imageRefs)
}

func fetchOCIImageRegistryTruth(
	ctx context.Context,
	reader GraphQuery,
	imageRefs []string,
) ([]map[string]any, error) {
	if reader == nil || len(imageRefs) == 0 {
		return nil, nil
	}

	digestRefs, tagRefs := splitOCIImageRefs(imageRefs)
	truth := make([]map[string]any, 0, len(imageRefs))
	if len(digestRefs) > 0 {
		rows, err := fetchOCIImageDigestRows(ctx, reader, sortedMapKeys(digestRefs))
		if err != nil {
			return nil, err
		}
		truth = append(truth, buildOCIDigestTruthRows(rows, digestRefs)...)
	}
	if len(tagRefs) > 0 {
		rows, err := fetchOCIImageTagRows(ctx, reader, tagRefs)
		if err != nil {
			return nil, err
		}
		truth = append(truth, buildOCITagTruthRows(rows)...)
	}

	sort.SliceStable(truth, func(i, j int) bool {
		if left, right := StringVal(truth[i], "image_ref"), StringVal(truth[j], "image_ref"); left != right {
			return left < right
		}
		return StringVal(truth[i], "digest") < StringVal(truth[j], "digest")
	})
	return truth, nil
}

// The OCI registry-truth reads deliberately use one anchoring clause per Cypher
// statement and join across labels application-side. The pinned NornicDB build
// mis-executes any read that places a second MATCH (or a cross-clause property
// join) between the anchor and the projection: the old two-MATCH digest query
// returned a null `coalesce(image.id, image.descriptor_id)` and the old
// three-MATCH tag query dropped every row (#5287, proven live over Bolt). Each
// template below is a single `MATCH … WHERE … RETURN … ORDER BY` shape, and the
// image↔repository and tag↔repository↔image joins run in Go.

// ociImageByDigestCypher is the single-clause per-label image lookup by digest.
// The verb `%s` is one of ociImageLookupLabels.
const ociImageByDigestCypher = `
MATCH (image:%s)
WHERE image.digest IN $digests
RETURN coalesce(image.id, image.descriptor_id) AS image_id,
       image.digest AS digest,
       image.repository_id AS repository_id,
       image.media_type AS media_type
ORDER BY digest`

// ociRepositoryByUIDCypher is the single-clause registry-repository lookup that
// resolves an image/tag `repository_id` to its registry metadata.
const ociRepositoryByUIDCypher = `
MATCH (repo:OciRegistryRepository)
WHERE repo.uid IN $repository_ids
RETURN repo.uid AS repository_id,
       repo.registry AS registry,
       repo.repository AS repository,
       repo.provider AS provider
ORDER BY repository_id`

// ociTagObservationByRefCypher is the single-clause tag-observation lookup that
// resolves a mutable tag reference to its recorded digest and repository.
const ociTagObservationByRefCypher = `
MATCH (tag:ContainerImageTagObservation)
WHERE tag.image_ref IN $image_refs
RETURN tag.image_ref AS image_ref,
       tag.tag AS tag,
       tag.resolved_digest AS digest,
       tag.repository_id AS repository_id
ORDER BY image_ref`

// fetchOCIImageDigestRows returns digest-addressed image registry truth by
// reading each image label with a single-clause query and joining the
// registry-repository metadata in Go. It preserves the old inner-join
// semantics (an image with no matching repository is omitted).
func fetchOCIImageDigestRows(
	ctx context.Context,
	reader GraphQuery,
	digests []string,
) ([]map[string]any, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	images, err := fetchOCIImagesByDigest(ctx, reader, digests)
	if err != nil {
		return nil, err
	}
	repos, err := fetchOCIRepositoriesByUID(ctx, reader, distinctFieldValues(images, "repository_id"))
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(images))
	for _, image := range images {
		repo, ok := repos[StringVal(image, "repository_id")]
		if !ok {
			continue
		}
		rows = append(rows, joinOCIImageRepository(image, repo))
	}
	return rows, nil
}

// fetchOCIImageTagRows returns tag-resolved image registry truth. It reads tag
// observations with a single-clause query, then joins the registry repository
// (by repository_id) and the canonical image (by resolved digest) in Go,
// preserving the old inner-join semantics (a tag whose repository or resolved
// image is absent is omitted).
func fetchOCIImageTagRows(
	ctx context.Context,
	reader GraphQuery,
	imageRefs []string,
) ([]map[string]any, error) {
	if len(imageRefs) == 0 {
		return nil, nil
	}
	tags, err := reader.Run(ctx, ociTagObservationByRefCypher, map[string]any{"image_refs": imageRefs})
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, nil
	}
	repos, err := fetchOCIRepositoriesByUID(ctx, reader, distinctFieldValues(tags, "repository_id"))
	if err != nil {
		return nil, err
	}
	images, err := fetchOCIImagesByDigest(ctx, reader, distinctFieldValues(tags, "digest"))
	if err != nil {
		return nil, err
	}
	imageByDigest := indexOCIImagesByDigest(images)
	rows := make([]map[string]any, 0, len(tags))
	for _, tag := range tags {
		repo, repoOK := repos[StringVal(tag, "repository_id")]
		image, imageOK := imageByDigest[StringVal(tag, "digest")]
		if !repoOK || !imageOK {
			continue
		}
		rows = append(rows, joinOCITagRepositoryImage(tag, repo, image))
	}
	return rows, nil
}

// fetchOCIImagesByDigest reads each OCI image label with a single-clause query
// and concatenates the rows. Each row carries image_id, digest, repository_id,
// and media_type.
func fetchOCIImagesByDigest(
	ctx context.Context,
	reader GraphQuery,
	digests []string,
) ([]map[string]any, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	rows := make([]map[string]any, 0, len(digests)*len(ociImageLookupLabels))
	for _, label := range ociImageLookupLabels {
		labelRows, err := reader.Run(ctx, fmt.Sprintf(ociImageByDigestCypher, label), map[string]any{"digests": digests})
		if err != nil {
			return nil, err
		}
		rows = append(rows, labelRows...)
	}
	return rows, nil
}

// fetchOCIRepositoriesByUID reads the registry repositories for the given uids
// with one single-clause query and indexes them by repository_id for the Go
// join.
func fetchOCIRepositoriesByUID(
	ctx context.Context,
	reader GraphQuery,
	uids []string,
) (map[string]map[string]any, error) {
	result := make(map[string]map[string]any, len(uids))
	if len(uids) == 0 {
		return result, nil
	}
	rows, err := reader.Run(ctx, ociRepositoryByUIDCypher, map[string]any{"repository_ids": uids})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if id := StringVal(row, "repository_id"); id != "" {
			result[id] = row
		}
	}
	return result, nil
}

// indexOCIImagesByDigest keeps the first image row seen per digest so a tag can
// resolve its canonical image identity and media type. Digest is the canonical
// content address, so the first-wins policy is deterministic under the ordered
// per-label reads.
func indexOCIImagesByDigest(images []map[string]any) map[string]map[string]any {
	byDigest := make(map[string]map[string]any, len(images))
	for _, image := range images {
		digest := StringVal(image, "digest")
		if digest == "" {
			continue
		}
		if _, exists := byDigest[digest]; !exists {
			byDigest[digest] = image
		}
	}
	return byDigest
}

// joinOCIImageRepository merges a digest-addressed image row with its registry
// repository into the row shape the digest truth builder consumes.
func joinOCIImageRepository(image, repo map[string]any) map[string]any {
	return map[string]any{
		"image_id":      StringVal(image, "image_id"),
		"digest":        StringVal(image, "digest"),
		"registry":      StringVal(repo, "registry"),
		"repository":    StringVal(repo, "repository"),
		"repository_id": StringVal(image, "repository_id"),
		"media_type":    StringVal(image, "media_type"),
		"provider":      StringVal(repo, "provider"),
	}
}

// joinOCITagRepositoryImage merges a tag observation with its registry
// repository and resolved image into the row shape the tag truth builder
// consumes. The digest and repository_id come from the tag observation, the
// registry metadata from the repository, and the image identity/media type from
// the resolved image.
func joinOCITagRepositoryImage(tag, repo, image map[string]any) map[string]any {
	return map[string]any{
		"image_ref":     StringVal(tag, "image_ref"),
		"tag":           StringVal(tag, "tag"),
		"digest":        StringVal(tag, "digest"),
		"image_id":      StringVal(image, "image_id"),
		"registry":      StringVal(repo, "registry"),
		"repository":    StringVal(repo, "repository"),
		"repository_id": StringVal(tag, "repository_id"),
		"media_type":    StringVal(image, "media_type"),
		"provider":      StringVal(repo, "provider"),
	}
}

func splitOCIImageRefs(imageRefs []string) (map[string][]string, []string) {
	digestRefs := make(map[string][]string)
	tagRefs := make([]string, 0, len(imageRefs))
	seenTags := make(map[string]struct{}, len(imageRefs))
	for _, imageRef := range imageRefs {
		imageRef = strings.TrimSpace(imageRef)
		if imageRef == "" {
			continue
		}
		if digest := imageRefDigest(imageRef); digest != "" {
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

func imageRefDigest(imageRef string) string {
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

func buildOCIDigestTruthRows(
	rows []map[string]any,
	digestRefs map[string][]string,
) []map[string]any {
	truth := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		digest := strings.ToLower(strings.TrimSpace(StringVal(row, "digest")))
		if digest == "" {
			continue
		}
		for _, imageRef := range digestRefs[digest] {
			truth = append(truth, ociTruthRow(row, imageRef, digest, ociDigestMatchStrength, "digest", false))
		}
	}
	return truth
}

func buildOCITagTruthRows(rows []map[string]any) []map[string]any {
	grouped := make(map[string][]map[string]any, len(rows))
	for _, row := range rows {
		imageRef := strings.TrimSpace(StringVal(row, "image_ref"))
		digest := strings.ToLower(strings.TrimSpace(StringVal(row, "digest")))
		if imageRef == "" || digest == "" {
			continue
		}
		grouped[imageRef] = append(grouped[imageRef], row)
	}

	imageRefs := sortedMapKeys(grouped)
	truth := make([]map[string]any, 0, len(imageRefs))
	for _, imageRef := range imageRefs {
		group := grouped[imageRef]
		digests := uniqueSortedRowValues(group, "digest")
		if len(digests) != 1 {
			truth = append(truth, map[string]any{
				"image_ref":          imageRef,
				"match_strength":     ociAmbiguousMatchStrength,
				"truth_basis":        "observed_tag",
				"identity_strength":  "weak_tag",
				"identity_source":    ociRegistryProjectionBasis,
				"ambiguous":          true,
				"digest_candidates":  digests,
				"registry":           StringVal(group[0], "registry"),
				"repository":         StringVal(group[0], "repository"),
				"repository_id":      StringVal(group[0], "repository_id"),
				"tag":                StringVal(group[0], "tag"),
				"resolved_row_count": len(group),
			})
			continue
		}
		truth = append(truth, ociTruthRow(group[0], imageRef, digests[0], ociTagMatchStrength, "tag_observation_with_digest", false))
	}
	return truth
}

func canonicalOCIImageMatchCount(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		if StringVal(row, "match_strength") == ociDigestMatchStrength {
			count++
		}
	}
	return count
}

func ociTruthRow(
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
		"image_id":          StringVal(row, "image_id"),
		"registry":          StringVal(row, "registry"),
		"repository":        StringVal(row, "repository"),
		"repository_id":     StringVal(row, "repository_id"),
		"media_type":        StringVal(row, "media_type"),
		"provider":          StringVal(row, "provider"),
		"match_strength":    matchStrength,
		"truth_basis":       truthBasis,
		"identity_source":   ociRegistryProjectionBasis,
		"identity_strength": "digest",
		"ambiguous":         ambiguous,
	}
	if tag := StringVal(row, "tag"); tag != "" {
		result["tag"] = tag
		result["identity_strength"] = "tag_observation_with_digest"
	}
	return result
}

func uniqueSortedRowValues(rows []map[string]any, key string) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = appendUniqueQueryString(values, strings.ToLower(strings.TrimSpace(StringVal(row, key))))
	}
	sort.Strings(values)
	return values
}

func sortedMapKeys[T any](values map[string]T) []string {
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
