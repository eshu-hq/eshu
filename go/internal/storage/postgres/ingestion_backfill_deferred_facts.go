// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// listDeferredScopedRelationshipFactRecordsQuery is the self-exclusion variant
// of listOnboardedRepoScopedRelationshipFactRecordsQuery used exclusively by the
// corpus-wide deferred backfill (issue #3659). It accepts seven parameters:
//
//	$1 pq.StringArray — LIKE terms derived from non-repo_id aliases (name,
//	   slug tokens) and the unconditional ArgoCD over-select markers. A fact
//	   matching $1 carries a cross-repo reference that is NOT keyed on its own
//	   repo_id, so loading it can produce evidence and it is always loaded.
//
//	$2 pq.StringArray — raw lowercase repo_id values. Retained as a typed
//	   compatibility parameter for callers and the previous proof shape; the
//	   current family-ID candidate path does not use the repo_id fallback arm.
//
//	$3 string — the scope_id partition this run is bounded to (issue #3710).
//
//	$4 string — the generation_id partition this run is bounded to (issue #3710).
//
//	$5 sql.NullString (nullable text) — retained as a typed compatibility
//	   parameter for the previous repo_id fast arm.
//
//	$6 string — retained as a typed compatibility parameter for the previous
//	   scope-derived repo_id performance hint.
//
//	$7 pq.StringArray — retained as a typed compatibility parameter for the
//	   previous relationship_reference_candidate_keys membership arm.
//
// The per-commit scoped query (listOnboardedRepoScopedRelationshipFactRecordsQuery)
// uses a single-parameter LIKE ANY and does not need self-exclusion because its
// anchorCatalog is the onboarding delta (new repos only): a new repo's own
// facts are not in the corpus yet, so its repo_id cannot self-match.
//
// Relationship-family ID surface (issue #5092). The deferred corpus backfill no
// longer scans every content/file/GCP payload in the scope partition. Accepted
// fact commits maintain relationship_family_candidate_fact_ids for facts whose
// artifact type, path, or ArgoCD marker belongs to a relationship extractor
// family. The query first narrows to that compact per-scope fact_id surface,
// applies the non-repo alias/path LIKE terms over those candidates only, then
// fetches full payloads for the matched ids. The $2/$5/$6/$7 parameters remain
// wired for call-site compatibility with the previous proof shape, but the
// rejected repo_id/reference-key fallback arms are not part of this hot path.
//
// Correctness invariant: the relationship-family predicate must be a superset
// of every fact family relationships.DiscoverEvidence can use. The DSN-gated
// proof compares this production query with a temp-table alias-only candidate
// and then runs DiscoverEvidence over OLD/NEW loads, requiring bidirectional
// evidence equality on task777, high-evidence scopes, and the ApplicationSet
// cross-scope case.
const listDeferredScopedRelationshipFactRecordsQuery = latestGenerationCTE + `,
arg_types AS (
    SELECT
        $2::text[] AS repo_ids,
        $5::text AS repo_regex,
        $6::text AS own_repo_id,
        $7::text[] AS repo_reference_keys
),
matched_fact_ids AS MATERIALIZED (
    SELECT fact.fact_id
    FROM relationship_family_candidate_fact_ids AS family
    JOIN fact_records AS fact
      ON fact.fact_id = family.fact_id
    JOIN arg_types ON TRUE
    WHERE family.scope_id = $3
      AND family.generation_id = $4
      AND lower(fact.payload::text) LIKE ANY($1)
)
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, '') AS source_uri,
    COALESCE(fact.source_record_id, '') AS source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN matched_fact_ids AS matched
  ON matched.fact_id = fact.fact_id
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// deferredScopedFactQueryParams holds the catalog-derived SQL parameters shared by
// every per-scope deferred fact-load query: the $1 non-repo_id LIKE terms and the
// $2 raw repo_id self-exclusion values. They are computed once from the full
// catalog and reused across partitions, so the per-scope fan-out does not rebuild
// them per query.
type deferredScopedFactQueryParams struct {
	nonRepoIDLike      pq.StringArray
	repoIDValues       pq.StringArray
	repoIDReferenceKey pq.StringArray
}

// buildDeferredScopedFactQueryParams derives the shared $1/$2 parameters from the
// catalog. It returns ok=false when neither arm has anything to match, signalling
// the caller to skip the fact load entirely: with no anchor and no repo_id value
// no content/file/gcp fact can resolve a catalog target.
func buildDeferredScopedFactQueryParams(
	catalog []relationships.CatalogEntry,
) (deferredScopedFactQueryParams, bool) {
	nonRepoIDTerms := backfillNonRepoIDAnchorTerms(catalog)
	repoIDValues := relationships.CatalogRepoIDValues(catalog)
	if len(nonRepoIDTerms) == 0 && len(repoIDValues) == 0 {
		return deferredScopedFactQueryParams{}, false
	}

	nonRepoIDLike := buildPayloadAnchorLikeTerms(nonRepoIDTerms)
	repoIDRaw := make([]string, 0, len(repoIDValues))
	repoIDReferenceKeys := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			repoIDRaw = append(repoIDRaw, value)
			repoIDReferenceKeys = append(repoIDReferenceKeys, relationships.CatalogReferenceKey(value))
		}
	}

	// Zero-length arrays are valid: LIKE ANY({}) is false, so an empty arm simply
	// never contributes a match.
	if nonRepoIDLike == nil {
		nonRepoIDLike = []string{}
	}
	if repoIDRaw == nil {
		repoIDRaw = []string{}
	}
	if repoIDReferenceKeys == nil {
		repoIDReferenceKeys = []string{}
	}

	return deferredScopedFactQueryParams{
		nonRepoIDLike:      pq.StringArray(nonRepoIDLike),
		repoIDValues:       pq.StringArray(repoIDRaw),
		repoIDReferenceKey: pq.StringArray(repoIDReferenceKeys),
	}, true
}

// loadDeferredScopedRelationshipFactsForPartition runs the family-ID relationship
// fact-load query bounded to one (scope_id, generation_id) partition. The
// catalog-derived $1 alias terms are shared across partitions; $3/$4 bind the
// partition. The old repo_id/reference-key parameters are still supplied with
// stable types so derived proof tests and callers keep one query call shape
// while the hot path uses only the relationship-family fact-id surface plus the
// alias terms.
func loadDeferredScopedRelationshipFactsForPartition(
	ctx context.Context,
	queryer Queryer,
	params deferredScopedFactQueryParams,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	ownRepoID := deferredScopedFactOwnRepoIDFromScope(scopeID)
	regex, ok := buildDeferredRepoIDRegex([]string(params.repoIDValues), ownRepoID)
	repoIDReferenceKeys := deferredRepoIDReferenceKeys(params.repoIDValues, params.repoIDReferenceKey)
	var regexParam sql.NullString
	if ok {
		regexParam = sql.NullString{String: regex, Valid: true}
	}

	rows, err := queryer.QueryContext(
		ctx,
		listDeferredScopedRelationshipFactRecordsQuery,
		params.nonRepoIDLike,
		params.repoIDValues,
		scopeID,
		generationID,
		regexParam,
		ownRepoID,
		repoIDReferenceKeys,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return loaded, nil
}

func deferredRepoIDReferenceKeys(repoIDValues, repoIDReferenceKeys pq.StringArray) pq.StringArray {
	if len(repoIDReferenceKeys) == len(repoIDValues) {
		return repoIDReferenceKeys
	}
	keys := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		keys = append(keys, relationships.CatalogReferenceKey(value))
	}
	return pq.StringArray(keys)
}
