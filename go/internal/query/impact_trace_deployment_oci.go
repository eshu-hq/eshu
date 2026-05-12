package query

import (
	"context"
	"sort"
	"strings"
)

const (
	ociDigestMatchStrength     = "canonical_digest"
	ociTagMatchStrength        = "tag_resolved_to_digest"
	ociAmbiguousMatchStrength  = "ambiguous_tag"
	ociRegistryProjectionBasis = "oci_registry_projection"
)

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

func fetchOCIImageDigestRows(
	ctx context.Context,
	reader GraphQuery,
	digests []string,
) ([]map[string]any, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	return reader.Run(ctx, `
		CALL {
			MATCH (image:ContainerImage)
			WHERE image.digest IN $digests
			RETURN image
			UNION
			MATCH (image:ContainerImageIndex)
			WHERE image.digest IN $digests
			RETURN image
			UNION
			MATCH (image:ContainerImageDescriptor)
			WHERE image.digest IN $digests
			RETURN image
		}
		MATCH (image)<-[:PUBLISHES_MANIFEST|PUBLISHES_INDEX|PUBLISHES_DESCRIPTOR]-(repo:OciRegistryRepository)
		RETURN DISTINCT coalesce(image.id, image.descriptor_id) AS image_id,
		       image.digest AS digest,
		       repo.registry AS registry,
		       repo.repository AS repository,
		       repo.uid AS repository_id,
		       image.media_type AS media_type,
		       repo.provider AS provider
		ORDER BY repository_id, digest
	`, map[string]any{"digests": digests})
}

func fetchOCIImageTagRows(
	ctx context.Context,
	reader GraphQuery,
	imageRefs []string,
) ([]map[string]any, error) {
	if len(imageRefs) == 0 {
		return nil, nil
	}
	return reader.Run(ctx, `
		MATCH (tag:ContainerImageTagObservation)
		WHERE tag.image_ref IN $image_refs
		MATCH (repo:OciRegistryRepository)-[:OBSERVED_TAG]->(tag)
		CALL {
			WITH tag
			MATCH (image:ContainerImage)
			WHERE image.digest = tag.resolved_digest
			RETURN image
			UNION
			WITH tag
			MATCH (image:ContainerImageIndex)
			WHERE image.digest = tag.resolved_digest
			RETURN image
			UNION
			WITH tag
			MATCH (image:ContainerImageDescriptor)
			WHERE image.digest = tag.resolved_digest
			RETURN image
		}
		RETURN DISTINCT tag.image_ref AS image_ref,
		       tag.tag AS tag,
		       tag.resolved_digest AS digest,
		       coalesce(image.id, image.descriptor_id) AS image_id,
		       repo.registry AS registry,
		       repo.repository AS repository,
		       repo.uid AS repository_id,
		       image.media_type AS media_type,
		       repo.provider AS provider
		ORDER BY image_ref, digest
	`, map[string]any{"image_refs": imageRefs})
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
