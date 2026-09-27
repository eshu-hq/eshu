// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the pure OCI registry-truth row builders: parsing image
// refs into digest- and tag-addressed keys, and shaping raw joined rows into
// truth rows. They issue no graph query, so they live apart from
// trace_deployment_oci.go (the three Run-calling fetchers) to keep that file
// under the repo's 500-line cap (#6590).

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
		digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "digest")))
		if digest == "" {
			continue
		}
		for _, imageRef := range digestRefs[digest] {
			truth = append(truth, ociTruthRow(row, imageRef, digest, deployment.OciDigestMatchStrength, "digest", false))
		}
	}
	return truth
}

func buildOCITagTruthRows(rows []map[string]any) []map[string]any {
	grouped := make(map[string][]map[string]any, len(rows))
	for _, row := range rows {
		imageRef := strings.TrimSpace(querycontract.StringVal(row, "image_ref"))
		digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "digest")))
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
				"registry":           querycontract.StringVal(group[0], "registry"),
				"repository":         querycontract.StringVal(group[0], "repository"),
				"repository_id":      querycontract.StringVal(group[0], "repository_id"),
				"tag":                querycontract.StringVal(group[0], "tag"),
				"resolved_row_count": len(group),
			})
			continue
		}
		truth = append(truth, ociTruthRow(group[0], imageRef, digests[0], ociTagMatchStrength, "tag_observation_with_digest", false))
	}
	return truth
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
		"image_id":          querycontract.StringVal(row, "image_id"),
		"registry":          querycontract.StringVal(row, "registry"),
		"repository":        querycontract.StringVal(row, "repository"),
		"repository_id":     querycontract.StringVal(row, "repository_id"),
		"media_type":        querycontract.StringVal(row, "media_type"),
		"provider":          querycontract.StringVal(row, "provider"),
		"match_strength":    matchStrength,
		"truth_basis":       truthBasis,
		"identity_source":   ociRegistryProjectionBasis,
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

// ociMaxKeysPerStatement is the per-statement IN-list bound these reads are
// recorded against in go/internal/queryplan/testdata/query-source-coverage.yaml
// (bounded_key_batch, max_keys: 250).
//
// The bound used to be an assumption rather than a property of the code (#6590).
// The keys are deduplicated upstream but their COUNT is not capped: they come
// from a workload row set capped at ServiceStoryItemLimit rows, but a single
// workload can declare any number of containers and initContainers, so 50
// workloads with six images each already puts 300 keys into one IN-list.
// Enforcing it here makes the recorded bound true by construction.
//
// Batching rather than truncating is deliberate: a truncated key set would
// silently drop images from the deployment trace, which is an accuracy loss.
// Capping at the source is also wrong -- collectContainerImages lives in the
// YAML parser, and a cap there would discard facts at ingest.
const ociMaxKeysPerStatement = 250

// ociKeyBatches splits keys into consecutive batches of at most
// ociMaxKeysPerStatement, preserving order. It is pure: it issues no statement.
//
// That is deliberate. The query-plan registry
// (go/internal/queryplan/testdata/query-source-coverage.yaml) attributes a
// graph read to the function that calls Run, and records a separate bound for
// each of these three reads. A shared helper that called Run itself collapsed
// three differently-bounded queries into one anonymous callsite and erased the
// per-query audit this bound exists for. So each fetcher keeps its own Run,
// looped over these batches, and remains its own registered callsite.
//
// Batching preserves every caller's semantics: each read is a keyed IN-list
// lookup, so all rows for one key land in the same batch; the tag and
// repository reads join through maps; and fetchOCIImagesByDigest batches
// inside its per-label loop, so indexOCIImagesByDigest's first-wins ordering
// across labels is unchanged. A key set within the bound is one batch, so it
// issues exactly the single statement it always did (plus any row-limit
// continuation statements #6590 adds when that one batch's fan-out overflows
// ociRegistryTruthRowLimit).
func ociKeyBatches(keys []string) [][]string {
	if len(keys) == 0 {
		return nil
	}
	batches := make([][]string, 0, (len(keys)+ociMaxKeysPerStatement-1)/ociMaxKeysPerStatement)
	for start := 0; start < len(keys); start += ociMaxKeysPerStatement {
		batches = append(batches, keys[start:min(start+ociMaxKeysPerStatement, len(keys))])
	}
	return batches
}

// indexOCIImagesByDigest keeps the first image row seen per digest so a tag can
// resolve its canonical image identity and media type. Digest is the canonical
// content address, so the first-wins policy is deterministic under the ordered
// per-label reads.
func indexOCIImagesByDigest(images []map[string]any) map[string]map[string]any {
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

// joinOCIImageRepository merges a digest-addressed image row with its registry
// repository into the row shape the digest truth builder consumes.
func joinOCIImageRepository(image, repo map[string]any) map[string]any {
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

// joinOCITagRepositoryImage merges a tag observation with its registry
// repository and resolved image into the row shape the tag truth builder
// consumes. The digest and repository_id come from the tag observation, the
// registry metadata from the repository, and the image identity/media type from
// the resolved image.
func joinOCITagRepositoryImage(tag, repo, image map[string]any) map[string]any {
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
