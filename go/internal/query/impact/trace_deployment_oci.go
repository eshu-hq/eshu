// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query/impact/oci"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ociImageLookupLabels are the OCI image node labels a digest can resolve
// to: a plain manifest, an index (multi-platform manifest list), or a bare
// descriptor. fetchOCIImagesByDigest substitutes each label into
// ociImageByDigestCypher's anchor in turn.
var ociImageLookupLabels = []string{
	"ContainerImage",
	"ContainerImageIndex",
	"ContainerImageDescriptor",
}

// FetchOCIImageRegistryTruth is the production entrypoint: it runs the
// bounded OCI registry-truth read and reports truncation to the operator
// signals (#6590). serviceName is a log field only.
func (h *Handler) FetchOCIImageRegistryTruth(
	ctx context.Context,
	imageRefs []string,
	serviceName string,
) (oci.RegistryTruthResult, error) {
	if h == nil || h.Neo4j == nil {
		return oci.RegistryTruthResult{}, nil
	}
	result, reasonEvents, err := fetchOCIImageRegistryTruthCore(ctx, h.Neo4j, imageRefs)
	if err != nil {
		return oci.RegistryTruthResult{}, err
	}
	h.reportOCIRegistryTruthTruncated(ctx, serviceName, result, reasonEvents)
	return result, nil
}

// reportOCIRegistryTruthTruncated fires the 3 AM operator signal for a
// bounded OCI registry-truth read that had to withhold image refs: a warn
// log (always, via h.Logger) per reason that actually truncated, and the
// eshu_dp_query_oci_registry_truth_truncated_total counter (when
// h.Instruments is wired), mirroring reportK8sSelectCandidatePoolTruncated
// (trace_deployment_k8s_select.go). service_name and
// truncated_image_ref_count are log fields, never metric attributes, so the
// counter stays low-cardinality; its only label is the bounded reason enum.
func (h *Handler) reportOCIRegistryTruthTruncated(
	ctx context.Context,
	serviceName string,
	result oci.RegistryTruthResult,
	reasonEvents map[string]int,
) {
	if len(result.TruncatedImageRefs) == 0 {
		return
	}
	for _, reason := range []string{oci.ReasonTagObservationRowLimit, oci.ReasonImageRowLimit} {
		events := reasonEvents[reason]
		if events <= 0 {
			continue
		}
		if h.Logger != nil {
			h.Logger.WarnContext(
				ctx, "OCI registry truth row limit reached; image refs withheld as truncated",
				"service_name", serviceName,
				"truncated_image_ref_count", len(result.TruncatedImageRefs),
				"statement_row_limit", oci.RegistryTruthRowLimit,
				"reason", reason,
			)
		}
		if h.Instruments != nil && h.Instruments.QueryOCIRegistryTruthTruncated != nil {
			h.Instruments.QueryOCIRegistryTruthTruncated.Add(ctx, int64(events),
				metric.WithAttributes(telemetry.AttrReason(reason)))
		}
	}
}

// FetchOCIImageRegistryTruth is the rows-only compatibility wrapper around
// FetchOCIImageRegistryTruthResult, kept for existing test callers and the
// impact_trace_deployment_oci_live_test.go live-truth test. Disclosure
// (truncation, limits) is discarded; production callers must use
// FetchOCIImageRegistryTruthResult (or the Handler method) instead.
func FetchOCIImageRegistryTruth(
	ctx context.Context,
	reader querycontract.GraphQuery,
	imageRefs []string,
) ([]map[string]any, error) {
	result, err := FetchOCIImageRegistryTruthResult(ctx, reader, imageRefs)
	return result.Rows, err
}

// FetchOCIImageRegistryTruthResult runs the bounded OCI registry-truth read
// (#6590) and returns the disclosed result: resolved truth rows, every image
// ref withheld because a statement's LIMIT $row_limit was reached, and the
// image_registry_truth_limits disclosure block.
func FetchOCIImageRegistryTruthResult(
	ctx context.Context,
	reader querycontract.GraphQuery,
	imageRefs []string,
) (oci.RegistryTruthResult, error) {
	result, _, err := fetchOCIImageRegistryTruthCore(ctx, reader, imageRefs)
	return result, err
}

// fetchOCIImageRegistryTruthCore is the shared implementation behind
// FetchOCIImageRegistryTruthResult and the Handler method: it also returns
// per-reason truncation event counts so the Handler can report them without
// growing oci.RegistryTruthResult beyond its three disclosed fields.
func fetchOCIImageRegistryTruthCore(
	ctx context.Context,
	reader querycontract.GraphQuery,
	imageRefs []string,
) (oci.RegistryTruthResult, map[string]int, error) {
	if reader == nil || len(imageRefs) == 0 {
		return oci.RegistryTruthResult{}, nil, nil
	}

	digestRefs, tagRefs := oci.SplitImageRefs(imageRefs)
	reasonEvents := make(map[string]int, 2)
	truth := make([]map[string]any, 0, len(imageRefs))
	var truncatedRefs []string

	if len(digestRefs) > 0 {
		rows, truncatedDigests, events, err := fetchOCIImageDigestRows(ctx, reader, oci.SortedMapKeys(digestRefs))
		if err != nil {
			return oci.RegistryTruthResult{}, nil, err
		}
		truth = append(truth, oci.BuildDigestTruthRows(rows, digestRefs)...)
		if events > 0 {
			reasonEvents[oci.ReasonImageRowLimit] += events
		}
		for _, digest := range truncatedDigests {
			truncatedRefs = append(truncatedRefs, digestRefs[digest]...)
		}
	}
	if len(tagRefs) > 0 {
		rows, withheldRefs, tagEvents, imageEvents, err := fetchOCIImageTagRows(ctx, reader, tagRefs)
		if err != nil {
			return oci.RegistryTruthResult{}, nil, err
		}
		truth = append(truth, oci.BuildTagTruthRows(rows)...)
		truncatedRefs = append(truncatedRefs, withheldRefs...)
		if tagEvents > 0 {
			reasonEvents[oci.ReasonTagObservationRowLimit] += tagEvents
		}
		if imageEvents > 0 {
			reasonEvents[oci.ReasonImageRowLimit] += imageEvents
		}
	}

	sort.SliceStable(truth, func(i, j int) bool {
		if left, right := querycontract.StringVal(truth[i], "image_ref"), querycontract.StringVal(truth[j], "image_ref"); left != right {
			return left < right
		}
		return querycontract.StringVal(truth[i], "digest") < querycontract.StringVal(truth[j], "digest")
	})
	truncatedRefs = oci.SortUniqueStrings(truncatedRefs)
	return oci.RegistryTruthResult{
		Rows:               truth,
		TruncatedImageRefs: truncatedRefs,
		Limits:             oci.RegistryTruthLimits(truncatedRefs),
	}, reasonEvents, nil
}

// The OCI registry-truth reads deliberately use one anchoring clause per Cypher
// statement and join across labels application-side. The pinned NornicDB build
// mis-executes any read that places a second MATCH (or a cross-clause property
// join) between the anchor and the projection: the old two-MATCH digest query
// returned a null `coalesce(image.id, image.descriptor_id)` and the old
// three-MATCH tag query dropped every row (#5287, proven live over Bolt). Each
// template below is a single `MATCH … WHERE … RETURN … ORDER BY … LIMIT` shape,
// and the image↔repository and tag↔repository↔image joins run in Go.

// ociImageByDigestCypher is the single-clause per-label image lookup by digest.
// The verb `%s` is one of ociImageLookupLabels. LIMIT $row_limit bounds the
// per-batch result set to oci.RegistryTruthRowLimit (#6590);
// oci.AdvanceBoundedRead resumes any batch it cuts off.
const ociImageByDigestCypher = `
MATCH (image:%s)
WHERE image.digest IN $digests
RETURN coalesce(image.id, image.descriptor_id) AS image_id,
       image.digest AS digest,
       image.repository_id AS repository_id,
       image.media_type AS media_type
ORDER BY digest, repository_id, image_id
LIMIT $row_limit`

// ociRepositoryByUIDCypher is the single-clause registry-repository lookup that
// resolves an image/tag `repository_id` to its registry metadata. No LIMIT: the
// batch key (OciRegistryRepository.uid) is schema-unique
// (oci_registry_repository_uid_unique), so fan-out is 1 by construction.
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
// LIMIT $row_limit bounds the per-batch result set to oci.RegistryTruthRowLimit
// (#6590); oci.AdvanceBoundedRead resumes any batch it cuts off.
const ociTagObservationByRefCypher = `
MATCH (tag:ContainerImageTagObservation)
WHERE tag.image_ref IN $image_refs
RETURN tag.image_ref AS image_ref,
       tag.tag AS tag,
       tag.resolved_digest AS digest,
       tag.repository_id AS repository_id
ORDER BY image_ref, digest, repository_id, tag
LIMIT $row_limit`

// fetchOCIImageDigestRows returns digest-addressed image registry truth by
// reading each image label with a single-clause, row-limit-bounded query and
// joining the registry-repository metadata in Go. It preserves the old
// inner-join semantics (an image with no matching repository is omitted).
// truncatedDigests names every digest whose image row set could not be
// resolved within oci.RegistryTruthRowLimit ON AT LEAST ONE LABEL; those
// digests contribute no rows from ANY label (#6590 gating-review P1, PR
// #7314). fetchOCIImagesByDigest issues one LIMIT statement per image label
// and unions every label's kept rows before reporting truncatedDigests, so a
// digest that overflowed on one label can still have rows on another; those
// surviving rows are dropped here, before shaping, so a withheld digest
// never emits a truth row from any label. events counts how many Run calls
// hit the bound (for telemetry).
func fetchOCIImageDigestRows(
	ctx context.Context,
	reader querycontract.GraphQuery,
	digests []string,
) (rows []map[string]any, truncatedDigests []string, events int, err error) {
	if len(digests) == 0 {
		return nil, nil, 0, nil
	}
	images, truncatedDigests, events, err := fetchOCIImagesByDigest(ctx, reader, digests)
	if err != nil {
		return nil, nil, 0, err
	}
	images = dropTruncatedDigestRows(images, truncatedDigests)
	repos, err := fetchOCIRepositoriesByUID(ctx, reader, distinctFieldValues(images, "repository_id"))
	if err != nil {
		return nil, nil, 0, err
	}
	rows = make([]map[string]any, 0, len(images))
	for _, image := range images {
		repo, ok := repos[querycontract.StringVal(image, "repository_id")]
		if !ok {
			continue
		}
		rows = append(rows, oci.JoinImageRepository(image, repo))
	}
	return rows, truncatedDigests, events, nil
}

// dropTruncatedDigestRows removes every row whose digest field is in
// truncatedDigests, so a digest withheld because one label's statement
// overflowed never surfaces a row a DIFFERENT label's statement returned
// (#6590 gating-review P1, PR #7314): the withheld-never-emitted contract
// applies per digest, not per label.
func dropTruncatedDigestRows(rows []map[string]any, truncatedDigests []string) []map[string]any {
	if len(truncatedDigests) == 0 {
		return rows
	}
	bad := make(map[string]struct{}, len(truncatedDigests))
	for _, digest := range truncatedDigests {
		bad[strings.ToLower(strings.TrimSpace(digest))] = struct{}{}
	}
	kept := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "digest")))
		if _, isBad := bad[digest]; isBad {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// fetchOCIImageTagRows returns tag-resolved image registry truth. It reads tag
// observations with a single-clause, row-limit-bounded query, then joins the
// registry repository (by repository_id) and the canonical image (by resolved
// digest) in Go, preserving the old inner-join semantics (a tag whose
// repository or resolved image is absent is omitted).
//
// withheldRefs names every image ref this call withholds because a statement
// hit oci.RegistryTruthRowLimit: refs whose own tag-observation read
// truncated, AND refs whose resolved digest's image read truncated (#6590 --
// truncating the digest read can flip a two-digest ref, since the tag row for
// the withheld digest would otherwise silently disappear through the
// existing inner join and leave a real ambiguous_tag looking
// tag_resolved_to_digest). tagEvents/imageEvents count how many Run calls hit
// the bound in each statement shape (for telemetry).
func fetchOCIImageTagRows(
	ctx context.Context,
	reader querycontract.GraphQuery,
	imageRefs []string,
) (rows []map[string]any, withheldRefs []string, tagEvents int, imageEvents int, err error) {
	if len(imageRefs) == 0 {
		return nil, nil, 0, 0, nil
	}
	tags := make([]map[string]any, 0, len(imageRefs))
	var truncatedTagRefs []string
	for _, batch := range oci.KeyBatches(oci.SortUniqueStrings(imageRefs)) {
		keysToQuery := batch
		for {
			batchRows, runErr := reader.Run(ctx, ociTagObservationByRefCypher, map[string]any{
				"image_refs": keysToQuery,
				"row_limit":  oci.RegistryTruthRowLimit,
			})
			if runErr != nil {
				return nil, nil, 0, 0, runErr
			}
			kept, truncated, next := oci.AdvanceBoundedRead(keysToQuery, batchRows, "image_ref", oci.RegistryTruthRowLimit)
			tags = append(tags, kept...)
			if len(truncated) > 0 {
				tagEvents++
				truncatedTagRefs = append(truncatedTagRefs, truncated...)
			}
			if next == nil {
				break
			}
			keysToQuery = next
		}
	}
	if len(tags) == 0 {
		return nil, oci.SortUniqueStrings(truncatedTagRefs), tagEvents, 0, nil
	}
	repos, err := fetchOCIRepositoriesByUID(ctx, reader, distinctFieldValues(tags, "repository_id"))
	if err != nil {
		return nil, nil, 0, 0, err
	}
	images, truncatedDigests, imageEvents, err := fetchOCIImagesByDigest(ctx, reader, distinctFieldValues(tags, "digest"))
	if err != nil {
		return nil, nil, 0, 0, err
	}

	withheld := make(map[string]struct{}, len(truncatedTagRefs))
	for _, ref := range truncatedTagRefs {
		withheld[ref] = struct{}{}
	}
	if len(truncatedDigests) > 0 {
		badDigests := make(map[string]struct{}, len(truncatedDigests))
		for _, digest := range truncatedDigests {
			badDigests[digest] = struct{}{}
		}
		for _, tag := range tags {
			digest := strings.ToLower(strings.TrimSpace(querycontract.StringVal(tag, "digest")))
			if _, bad := badDigests[digest]; bad {
				withheld[querycontract.StringVal(tag, "image_ref")] = struct{}{}
			}
		}
	}

	imageByDigest := oci.IndexImagesByDigest(images)
	rows = make([]map[string]any, 0, len(tags))
	for _, tag := range tags {
		ref := querycontract.StringVal(tag, "image_ref")
		if _, bad := withheld[ref]; bad {
			continue
		}
		repo, repoOK := repos[querycontract.StringVal(tag, "repository_id")]
		image, imageOK := imageByDigest[querycontract.StringVal(tag, "digest")]
		if !repoOK || !imageOK {
			continue
		}
		rows = append(rows, oci.JoinTagRepositoryImage(tag, repo, image))
	}
	withheldRefs = make([]string, 0, len(withheld))
	for ref := range withheld {
		withheldRefs = append(withheldRefs, ref)
	}
	sort.Strings(withheldRefs)
	return rows, withheldRefs, tagEvents, imageEvents, nil
}

// fetchOCIImagesByDigest reads each OCI image label with a single-clause,
// row-limit-bounded query and concatenates the rows. Each row carries
// image_id, digest, repository_id, and media_type. truncatedDigests names
// every digest whose row set could not be resolved within
// oci.RegistryTruthRowLimit for at least one label; those digests contribute
// no rows for that label. events counts how many Run calls hit the bound.
//
// digests is sorted and deduplicated on entry through oci.SortUniqueStrings:
// some callers pass an already-sorted key set (oci.SortedMapKeys(digestRefs))
// and some pass an insertion-ordered one (distinctFieldValues), and
// oci.AdvanceBoundedRead's irreducible-overflow check depends on every
// batch's first key being its minimum.
func fetchOCIImagesByDigest(
	ctx context.Context,
	reader querycontract.GraphQuery,
	digests []string,
) (rows []map[string]any, truncatedDigests []string, events int, err error) {
	if len(digests) == 0 {
		return nil, nil, 0, nil
	}
	sortedDigests := oci.SortUniqueStrings(digests)
	rows = make([]map[string]any, 0, len(sortedDigests)*len(ociImageLookupLabels))
	var truncated []string
	for _, label := range ociImageLookupLabels {
		for _, batch := range oci.KeyBatches(sortedDigests) {
			keysToQuery := batch
			for {
				labelRows, runErr := reader.Run(ctx, fmt.Sprintf(ociImageByDigestCypher, label), map[string]any{
					"digests":   keysToQuery,
					"row_limit": oci.RegistryTruthRowLimit,
				})
				if runErr != nil {
					return nil, nil, 0, runErr
				}
				kept, truncatedBatch, next := oci.AdvanceBoundedRead(keysToQuery, labelRows, "digest", oci.RegistryTruthRowLimit)
				rows = append(rows, kept...)
				if len(truncatedBatch) > 0 {
					events++
					truncated = append(truncated, truncatedBatch...)
				}
				if next == nil {
					break
				}
				keysToQuery = next
			}
		}
	}
	return rows, oci.SortUniqueStrings(truncated), events, nil
}

// fetchOCIRepositoriesByUID reads the registry repositories for the given uids
// with one single-clause query and indexes them by repository_id for the Go
// join. No row-limit bound: OciRegistryRepository.uid is schema-unique, so
// each key can match at most one row (fan_out_multiplier: 1 in
// query-source-coverage.yaml).
func fetchOCIRepositoriesByUID(
	ctx context.Context,
	reader querycontract.GraphQuery,
	uids []string,
) (map[string]map[string]any, error) {
	result := make(map[string]map[string]any, len(uids))
	if len(uids) == 0 {
		return result, nil
	}
	for _, batch := range oci.KeyBatches(uids) {
		rows, err := reader.Run(ctx, ociRepositoryByUIDCypher, map[string]any{"repository_ids": batch})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if id := querycontract.StringVal(row, "repository_id"); id != "" {
				result[id] = row
			}
		}
	}
	return result, nil
}
