// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"strings"
)

// Producer-activation dependency index SQL (#7635). The per-domain table lives
// in ingestion_producer_activation.go; this file holds the queries the index
// runs. All key extraction is inline SQL composed from the fragments below —
// no database functions, so no extra migration — mirroring the Go join code
// field for field:
//
//   - repository keys mirror sourceRepositoryKey
//     (kubernetes_correlation_index.go): repository_id when set, else
//     registry/repository, minus the oci-registry:// prefix, trimmed and
//     lowercased.
//   - image references mirror ParseContainerImageRef
//     (container_image_identity_ref_parsing.go): digest form splits at the
//     first '@' with a sha256: digest; tag form splits at the last colon
//     past the last slash. TestProducerImageRefParseParity pins the SQL
//     mirror against the Go parser over a corpus of references.
//   - dockerfile base refs mirror dockerfileStageBaseRef
//     (container_image_base_image.go): trimmed base_image, scratch and
//     $-containing bases rejected, base_tag rejoined with ':'.
//   - drift ARN linkage mirrors listActiveStateResourcesForAWSARNsQuery:
//     the terraform_state_resource attributes.arn equated to the aws_resource
//     arn, both blank-trimmed and non-blank, with no tombstone filter on
//     either side because the reader has none.
//
// Arms with no exact SQL mirror (content_entity container_images,
// aws_relationship-to-image) are deliberately absent from key extraction:
// their consumers stay on the epoch whole pass rather than risk a fuzzy
// match storm. Digest linkage is digest-alone on purpose: digests are
// content-addressed, so the same digest in two repositories is the same
// image and repository qualification adds nothing. Tag linkage is
// (repo_key, tag) qualified because tags are repo-scoped.
//
// Whitespace corner: Go strings.TrimSpace strips unicode whitespace while
// SQL trim() strips spaces only, and typed payload decodes are assumed to
// preserve tag/digest strings verbatim. References carrying tabs, newlines,
// or padded tags/digests may link in SQL where Go misses or vice versa; the
// miss direction degrades to the epoch whole pass and the match direction
// replays idempotently. The parity test covers realistic references.

// producerPayloadStr mirrors payloadcore.PayloadStr for a top-level string
// field: the JSON string value space-trimmed, NULL when missing or blank.
func producerPayloadStr(payload, field string) string {
	return fmt.Sprintf("NULLIF(btrim((%s)->>'%s', ' '), '')", payload, field)
}

// producerRepoKeySQL mirrors sourceRepositoryKey: repository_id when set,
// else registry/repository composed with the oci-registry:// scheme, minus
// that scheme prefix, trimmed of spaces and slashes, lowercased. NULL when
// the payload names no repository.
func producerRepoKeySQL(payload string) string {
	repoid := fmt.Sprintf(`COALESCE(%s, CASE WHEN %s IS NOT NULL AND %s IS NOT NULL `+
		`THEN 'oci-registry://' || %s || '/' || %s END)`,
		producerPayloadStr(payload, "repository_id"),
		producerPayloadStr(payload, "registry"), producerPayloadStr(payload, "repository"),
		producerPayloadStr(payload, "registry"), producerPayloadStr(payload, "repository"))
	return fmt.Sprintf(`NULLIF(lower(trim(both '/' from trim(both ' ' from `+
		`CASE WHEN (%s) LIKE 'oci-registry://%%' THEN substring((%s) from 16) ELSE COALESCE((%s), '') END))), '')`,
		repoid, repoid, repoid)
}

// producerLastPosSQL is the 1-based index of the last occurrence of char in
// the expression, or 0 when absent.
func producerLastPosSQL(expr, char string) string {
	return fmt.Sprintf(`(CASE WHEN strpos((%s), '%s') = 0 THEN 0 `+
		`ELSE length((%s)) - strpos(reverse((%s)), '%s') + 1 END)`, expr, char, expr, expr, char)
}

// producerParsedRefBody is the SELECT list parsing one image reference into
// (repo_key, tag, digest), mirroring ParseContainerImageRef. ref is the
// trimmed non-empty reference expression. Unparseable refs yield NULLs;
// tags are not trimmed, matching Go.
func producerParsedRefBody(ref string) string {
	digestForm := fmt.Sprintf(`(strpos((%s), '@') > 0 AND substr((%s), strpos((%s), '@') + 1) LIKE 'sha256:%%')`,
		ref, ref, ref)
	lastColon := producerLastPosSQL(ref, ":")
	lastSlash := producerLastPosSQL(ref, "/")
	// Go returns early for digest form, so the tag arm must not fire
	// there: "repo@sha256:abc" carries a colon past the last slash but
	// has no tag.
	tagForm := fmt.Sprintf(`(NOT %s AND (%s) > (%s) AND (%s) < length((%s)))`, digestForm, lastColon, lastSlash, lastColon, ref)
	repoOf := func(s string) string {
		return fmt.Sprintf(`NULLIF(lower(trim(both '/' from trim(both ' ' from (%s)))), '')`, s)
	}
	return fmt.Sprintf(`CASE WHEN %s THEN %s WHEN %s THEN %s END AS repo_key, `+
		`CASE WHEN %s THEN substring((%s) from (%s) + 1) END AS tag, `+
		`CASE WHEN %s THEN substr((%s), strpos((%s), '@') + 1) END AS digest`,
		digestForm, repoOf(fmt.Sprintf(`split_part((%s), '@', 1)`, ref)),
		tagForm, repoOf(fmt.Sprintf(`left((%s), (%s) - 1)`, ref, lastColon)),
		tagForm, ref, lastColon,
		digestForm, ref, ref)
}

// producerDockerRefSQL rejoins one dockerfile stage's base_image/base_tag,
// mirroring dockerfileStageBaseRef. stage is the stage object expression.
// Rejected bases yield NULL.
func producerDockerRefSQL(stage string) string {
	image := fmt.Sprintf(`btrim((%s)->>'base_image', ' ')`, stage)
	tag := fmt.Sprintf(`btrim((%s)->>'base_tag', ' ')`, stage)
	return fmt.Sprintf(`(CASE WHEN (%s) = '' OR (%s) IS NULL OR (%s) LIKE '%%$%%' OR lower((%s)) = 'scratch' THEN NULL `+
		`WHEN (%s) <> '' AND (%s) IS NOT NULL THEN (%s) || ':' || (%s) ELSE (%s) END)`,
		image, image, image, image, tag, tag, image, tag, image)
}

// producerFactKeysLateral returns the LATERAL key-row source for one fact
// row: the UNION ALL of per-arm (repo_key, tag, digest) extracts, each arm
// guarded by its kind predicate so a fact yields only its own arms. fact is
// the fact_records alias. Callers join or filter the keys.
func producerFactKeysLateral(fact string) string {
	payload := fact + ".payload"
	repoKey := producerRepoKeySQL(payload)
	digest := producerPayloadStr(payload, "digest")
	tag := producerPayloadStr(payload, "tag")
	resolved := producerPayloadStr(payload, "resolved_digest")
	previous := producerPayloadStr(payload, "previous_digest")
	imageDigest := producerPayloadStr(payload, "image_digest")
	manifestDigest := producerPayloadStr(payload, "manifest_digest")
	kind := fact + ".fact_kind"
	system := fact + ".source_system"
	parsed := func(ref string) string {
		return fmt.Sprintf(`SELECT %s`, producerParsedRefBody(ref))
	}
	stages := fmt.Sprintf(`jsonb_array_elements(CASE WHEN jsonb_typeof((%s)->'dockerfile_stages') = 'array' `+
		`THEN (%s)->'dockerfile_stages' ELSE '[]'::jsonb END)`, payload, payload)
	refs := fmt.Sprintf(`jsonb_array_elements_text(CASE WHEN jsonb_typeof((%s)->'image_refs') = 'array' `+
		`THEN (%s)->'image_refs' ELSE '[]'::jsonb END)`, payload, payload)
	containers := fmt.Sprintf(`jsonb_array_elements(CASE WHEN jsonb_typeof((%s)->'containers') = 'array' `+
		`THEN (%s)->'containers' ELSE '[]'::jsonb END)`, payload, payload)
	dockerRef := producerDockerRefSQL("stage.value")
	return fmt.Sprintf(`LATERAL (
SELECT %s AS repo_key, NULL::text AS tag, %s AS digest WHERE %s IN ('oci_registry.image_manifest', 'oci_registry.image_index') AND %s = 'oci_registry'
UNION ALL
SELECT %s, %s, %s WHERE %s = 'oci_registry.image_tag_observation' AND %s = 'oci_registry'
UNION ALL
SELECT %s, NULL::text, %s WHERE %s = 'oci_registry.image_tag_observation' AND %s = 'oci_registry'
UNION ALL
SELECT NULL::text, NULL::text, %s WHERE (%s = 'aws_image_reference' AND %s = 'aws') OR (%s = 'azure_image_reference' AND %s = 'azure') OR (%s = 'gcp_image_reference' AND %s = 'gcp')
UNION ALL
SELECT NULL::text, NULL::text, %s WHERE %s = 'aws_image_reference' AND %s = 'aws'
UNION ALL
%s FROM %s AS stage(value) WHERE %s = 'file' AND %s = 'git'
UNION ALL
%s FROM %s AS ref(value) WHERE %s = 'kubernetes_live.pod_template'
UNION ALL
%s FROM %s AS container(value) WHERE %s = 'kubernetes_live.pod_template'
UNION ALL
%s FROM %s AS container(value) WHERE %s = 'kubernetes_live.pod_template'
)`,
		repoKey, digest, kind, system,
		repoKey, tag, resolved, kind, system,
		repoKey, previous, kind, system,
		imageDigest, kind, system, kind, system, kind, system,
		manifestDigest, kind, system,
		parsed(fmt.Sprintf(`NULLIF(btrim(%s, ' '), '')`, dockerRef)), stages, kind, system,
		parsed(`NULLIF(btrim(ref.value, ' '), '')`), refs, kind,
		parsed(`NULLIF(btrim(container.value->>'image', ' '), '')`), containers, kind,
		parsed(`NULLIF(btrim(container.value->>'resolved_image_digest', ' '), '')`), containers, kind,
	)
}

// producerKeyedFactKinds bounds the owed and consumer key scans to the fact
// kinds the linkage extracts keys from, so both probes ride the
// (scope_id, generation_id, fact_kind) index instead of scanning the
// generation's facts.
const producerKeyedFactKinds = `('oci_registry.image_manifest', 'oci_registry.image_index', 'oci_registry.image_tag_observation', 'aws_image_reference', 'azure_image_reference', 'gcp_image_reference', 'file', 'kubernetes_live.pod_template')`

// producerOwedOCIKeysQuery reads the owed generation's OCI linkage keys once
// per settle. The keys are invariant for the settle, so the settle fetches
// them in one indexed probe and passes them as arrays to the listing below;
// joining them inside the correlated EXISTS would rescan the owed
// generation's facts once per floored candidate.
var producerOwedOCIKeysQuery = `
SELECT keys.repo_key, keys.tag, keys.digest
FROM fact_records AS owed_fact, ` + producerFactKeysLateral("owed_fact") + ` AS keys
WHERE owed_fact.scope_id = $1
  AND owed_fact.generation_id = $2
  AND owed_fact.is_tombstone = FALSE
  AND owed_fact.fact_kind IN ` + producerKeyedFactKinds + `
`

// producerOCILinkageConjunct narrows the shipped correlation listing to the
// items whose scope holds active OCI linkage keys intersecting the owed
// generation's keys, completed before the obligation was owed ($2). The
// EXISTS is correlated on work.scope_id: the settle probes only the
// candidate scopes' active facts, never the corpus. The owed keys arrive as
// arrays ($3 digests, $4/$5 the (repo_key, tag) pairs in lockstep) fetched
// once by producerOwedOCIKeysQuery.
//
// Clock skew: $2 is DB clock_timestamp() while work.updated_at is the
// multi-host app clock, so the bound assumes NTP-bounded skew. Reducer-ahead
// skew skips linked items that completed in the skew window (missed replay
// until the next commit-driven epoch pass); DB-ahead skew double-pays
// post-Ack items the epoch pass already reopened (drift). The next epoch
// pass backstops both directions; no margin, per arbiter R2-A's exact bound.
var producerOCILinkageConjunct = `
  AND work.updated_at < $2::timestamptz
  AND EXISTS (
    SELECT 1
    FROM fact_records AS consumer_fact
    JOIN ingestion_scopes AS consumer_scope
      ON consumer_scope.scope_id = consumer_fact.scope_id
     AND consumer_scope.active_generation_id = consumer_fact.generation_id
    JOIN scope_generations AS consumer_generation
      ON consumer_generation.scope_id = consumer_fact.scope_id
     AND consumer_generation.generation_id = consumer_fact.generation_id,
    ` + producerFactKeysLateral("consumer_fact") + ` AS ckeys
    WHERE consumer_fact.scope_id = work.scope_id
      AND consumer_fact.is_tombstone = FALSE
      AND consumer_fact.fact_kind IN ` + producerKeyedFactKinds + `
      AND consumer_generation.status = 'active'
      AND (ckeys.digest = ANY($3::text[])
        OR (ckeys.repo_key, ckeys.tag) IN (SELECT * FROM unnest($4::text[], $5::text[]) AS pairs(repo_key, tag)))
  )
`

// producerOwedDriftARNsQuery reads the owed generation's
// terraform_state_resource ARNs once per settle, for the same rescan reason
// as producerOwedOCIKeysQuery. It returns the untrimmed attribute values
// filtered to non-blank-after-trim, mirroring the drift reader's equality.
const producerOwedDriftARNsQuery = `
SELECT DISTINCT state_fact.payload->'attributes'->>'arn'
FROM fact_records AS state_fact
WHERE state_fact.scope_id = $1
  AND state_fact.generation_id = $2
  AND state_fact.fact_kind = 'terraform_state_resource'
  AND btrim(COALESCE(state_fact.payload->'attributes'->>'arn', '')) <> ''
`

// producerDriftLinkageConjunct narrows the shipped correlation listing to the
// drift items whose own generation holds aws_resource ARNs intersecting the
// owed generation's terraform_state_resource ARNs ($3, fetched once by
// producerOwedDriftARNsQuery), completed before the obligation was owed
// ($2). It reads the item's own generation, mirroring the sealed-generation
// drift evidence query, not the scope's active one. The $2 staleness bound
// carries the same NTP-skew assumption documented on
// producerOCILinkageConjunct above.
const producerDriftLinkageConjunct = `
  AND work.updated_at < $2::timestamptz
  AND EXISTS (
    SELECT 1
    FROM fact_records AS aws_fact
    WHERE aws_fact.scope_id = work.scope_id
      AND aws_fact.generation_id = work.generation_id
      AND aws_fact.fact_kind = 'aws_resource'
      AND btrim(COALESCE(aws_fact.payload->>'arn', '')) <> ''
      AND aws_fact.payload->>'arn' = ANY($3::text[])
  )
`

// listProducerDependentOCIItemsQuery is the shipped correlation reopen
// listing AND the OCI linkage + staleness conjuncts: the floored succeeded
// items of $1 whose scope's active OCI keys intersect the owed keys
// ($3 digests, $4/$5 repo_key/tag pairs) and that completed before $2.
// TestProducerListingsDeriveFromShipped pins the derivation byte for byte.
var listProducerDependentOCIItemsQuery = deriveQueryAtMarker(
	listSucceededReducerWorkItemsByDomainQuery,
	correlationReopenStageMarker,
	producerOCILinkageConjunct,
)

// listProducerDependentDriftItemsQuery is the shipped correlation reopen
// listing AND the drift linkage + staleness conjuncts. The domain parameter
// ($1) is always aws_cloud_runtime_drift; $2 is the owed-at bound and $3
// the owed ARNs. The derivation keeps the shipped replay floor and
// failed-generation exclusion.
var listProducerDependentDriftItemsQuery = deriveQueryAtMarker(
	listSucceededReducerWorkItemsByDomainQuery,
	correlationReopenStageMarker,
	producerDriftLinkageConjunct,
)

// producerEvidenceCoreSQL is the producer-evidence predicate: identity-filter
// facts (embedded verbatim with the loader's own 'fact' alias, mirroring
// the loader including its tombstone filter) or terraform_state_resource
// facts with a joinable ARN (mirroring the drift reader, which has no
// tombstone filter).
var producerEvidenceCoreSQL = `(` + identityFactFilterSQL + ` AND fact.is_tombstone = FALSE)
      OR (fact.fact_kind = 'terraform_state_resource'
          AND btrim(COALESCE(fact.payload->'attributes'->>'arn', '')) <> '')`

// producerEvidenceFactKindPattern finds the fact_kind literals the identity
// filter matches: the IN-list arm and the equality arms.
var producerEvidenceFactKindPattern = regexp.MustCompile(`fact_kind\s+IN\s*\(([^)]*)\)|fact_kind\s*=\s*'([^']*)'`)

// producerEvidenceFactKindsFromFilter extracts every fact_kind literal the
// identity filter can match, in filter order. The probe's kind prefilter is
// derived from the filter text itself, never hand-copied, so a new filter
// arm cannot silently fall outside the prefilter.
func producerEvidenceFactKindsFromFilter(filter string) []string {
	var kinds []string
	for _, match := range producerEvidenceFactKindPattern.FindAllStringSubmatch(filter, -1) {
		if match[1] != "" {
			for _, item := range strings.Split(match[1], ",") {
				kinds = append(kinds, strings.Trim(strings.TrimSpace(item), "'"))
			}
			continue
		}
		kinds = append(kinds, match[2])
	}
	return kinds
}

// producerEvidenceFactKindListSQL is the comma-separated quoted kind list
// for the probe's prefilter: every kind the identity filter matches plus
// the drift arm's kind. Every OR arm below pins fact_kind, so the prefilter
// is implied by the predicate and cannot change the probe outcome (pinned
// by TestProducerEvidenceKindPrefilterDifferential); it lets the planner
// seek fact_records_scope_generation_idx on
// (scope_id, generation_id, fact_kind) instead of fetching the generation's
// non-producer facts only to filter them out (F1: a 5000-row non-producer
// generation costs 882 buffers unprefiltered, 4 prefiltered).
func producerEvidenceFactKindListSQL() string {
	kinds := append(producerEvidenceFactKindsFromFilter(identityFactFilterSQL), "terraform_state_resource")
	quoted := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		quoted = append(quoted, "'"+kind+"'")
	}
	return strings.Join(quoted, ", ")
}

// producerEvidenceExistsQuery reports whether the generation carries
// evidence a correlation consumer reads. The settle runs it once per
// obligation and retires generations without producer evidence as
// inapplicable; Ack owes unconditionally and never runs it (F1: ~8.4 ms of
// planning per call belongs on the background runner, not in Ack).
var producerEvidenceExistsQuery = `
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS fact
  WHERE fact.scope_id = $1
    AND fact.generation_id = $2
    AND fact.fact_kind IN (` + producerEvidenceFactKindListSQL() + `)
    AND (
      ` + producerEvidenceCoreSQL + `
    )
)`

// producerEvidenceExistsUnprefilteredQuery is the probe without the kind
// prefilter. It exists only for
// TestProducerEvidenceKindPrefilterDifferential, which proves the prefilter
// never changes the outcome over a corpus carrying every arm kind.
var producerEvidenceExistsUnprefilteredQuery = `
SELECT EXISTS (
  SELECT 1
  FROM fact_records AS fact
  WHERE fact.scope_id = $1
    AND fact.generation_id = $2
    AND (
      ` + producerEvidenceCoreSQL + `
    )
)`

// producerParseRefProbeSQL parses $1 as an image reference. It exists only
// for TestProducerImageRefParseParity, which pins the SQL mirror against
// ParseContainerImageRef.
var producerParseRefProbeSQL = `SELECT * FROM (` +
	`SELECT ` + producerParsedRefBody(`NULLIF(btrim($1, ' '), '')`) +
	`) AS parsed`
