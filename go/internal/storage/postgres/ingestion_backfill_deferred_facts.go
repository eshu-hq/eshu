package postgres

import (
	"context"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// listDeferredScopedRelationshipFactRecordsQuery is the self-exclusion variant
// of listOnboardedRepoScopedRelationshipFactRecordsQuery used exclusively by the
// corpus-wide deferred backfill (issue #3659). It accepts two parameters:
//
//	$1 pq.StringArray — LIKE terms derived from non-repo_id aliases (name,
//	   slug tokens) and the unconditional ArgoCD over-select markers. A fact
//	   matching $1 carries a cross-repo reference that is NOT keyed on its own
//	   repo_id, so loading it can produce evidence and it is always loaded.
//
//	$2 pq.StringArray — raw lowercase repo_id values. The repo_id arm uses
//	   EXISTS to load a fact only when its payload contains a catalog repo_id
//	   value that is not the row's own repo_id.
//
//	$3 string — the scope_id partition this run is bounded to (issue #3710).
//
//	$4 string — the generation_id partition this run is bounded to (issue #3710).
//
// Why EXISTS, not blind replace(): replace() corrupts overlap cases where the
// own repo_id is a prefix of another repo's repo_id (for example app vs
// app-config). EXISTS compares whole repo_id values, so the overlapping target
// still matches. The boundary test is a plain LIKE substring rather than a
// boundary regex (see the const's plan-shape comment): LIKE widens the SQL to a
// provable superset and the in-memory catalogMatcher re-applies boundary-safe
// token matching, so a repo_id that is a substring of another (app vs app-config)
// is over-selected at SQL but still resolved to the correct whole-value match by
// the matcher.
//
// Why not a value-exclusion list (payload->>'repo_id' != ALL($2)): every ACTIVE
// repo's own repo_id is in the catalog, so that predicate would exclude EVERY
// active repo's fact from the repo_id arm — including legitimate cross-repo
// references — also breaking truth-equivalence. The EXISTS test excludes only
// the row's exact self-value while keeping all OTHER repos' repo_id matches.
//
// Correctness invariant (truth-equivalence): a fact in repo A that references
// repo B's repo_id (A ≠ B, including B that contains A as a substring) has B's
// value present and B <> A, so EXISTS is true and the fact is loaded. A fact
// whose only repo_id match is its own (A) has no OTHER catalog repo_id present,
// so EXISTS is false and it is dropped — and the in-memory catalogMatcher would
// have skipped that self-match anyway (entry.RepoID == sourceRepoID), so no
// evidence the full-corpus load would have produced is dropped.
//
// The per-commit scoped query (listOnboardedRepoScopedRelationshipFactRecordsQuery)
// uses a single-parameter LIKE ANY and does not need self-exclusion because its
// anchorCatalog is the onboarding delta (new repos only): a new repo's own
// facts are not in the corpus yet, so its repo_id cannot self-match.
//
// Performance shape (issue #3710). The query is bound to one (scope_id,
// generation_id) partition via $3/$4 and selects from fact_records inside a
// `WITH matched_facts AS MATERIALIZED (...)` CTE. Two plan facts drove this shape,
// both measured by EXPLAIN ANALYZE on a 3.5M-fact Postgres 18 corpus:
//
//  1. Per-scope partition. The original query scanned every latest-generation
//     content/file/gcp fact once and evaluated the per-row self-exclusion arm
//     against the whole catalog: an O(facts × catalog) correlated scan that ran
//     ~20min+ at corpus scale (the measured long pole). The scope_generation
//     predicate ($3/$4) bounds each run to one repository's facts via
//     fact_records_scope_generation_idx, turning the monolithic scan into many
//     small ones the caller fans out across the deferred-maintenance worker pool.
//
//  2. MATERIALIZED candidate set. Without MATERIALIZED the planner joins
//     fact_records to latest_generations as a Nested Loop and pushes the
//     payload-text predicate to the inner side as a per-row Filter, which ignores
//     the trigram GIN index on lower(payload::text)
//     (fact_records_payload_trgm_idx). MATERIALIZED forces Postgres to build the
//     predicate-narrowed candidate set first — letting the $1 LIKE ANY arm drive a
//     Bitmap Index Scan — then join the small result to the latest-generation set.
//
// Self-exclusion arm ($2). The repo_id arm keeps the exact #3659 self-exclusion
// (catalog_repo_id.value <> the row's own repo_id) so a fact is never loaded
// solely because its own repo_id appears in the catalog. The boundary test is a
// plain substring `lower(payload::text) LIKE '%' || value || '%'` rather than the
// prior per-row boundary regex: the LIKE form widens the SQL result to a provable
// SUPERSET of the regex result (every boundary-bounded match is also a substring
// match), and the in-memory catalogMatcher (relationships.DiscoverEvidence ->
// catalogMatcher.match) re-applies the boundary-safe token matching and the
// self-match drop (entry.RepoID == sourceRepoID), so the final
// relationship-evidence set is identical (truth-equivalence). The regex was
// un-indexable and ~15x slower per scope than the LIKE form; both feed the same
// matcher.
const listDeferredScopedRelationshipFactRecordsQuery = latestGenerationCTE + `,
matched_facts AS MATERIALIZED (
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
    WHERE fact.scope_id = $3
      AND fact.generation_id = $4
      AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND (
        lower(fact.payload::text) LIKE ANY($1)
        OR EXISTS (
          SELECT 1
          FROM unnest($2::text[]) AS catalog_repo_id(value)
          WHERE catalog_repo_id.value <> lower(COALESCE(fact.payload->>'repo_id', ''))
            AND lower(fact.payload::text) LIKE '%' || catalog_repo_id.value || '%'
        )
      )
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
    fact.source_uri,
    fact.source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM matched_facts AS fact
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
	nonRepoIDLike pq.StringArray
	repoIDValues  pq.StringArray
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
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			repoIDRaw = append(repoIDRaw, value)
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

	return deferredScopedFactQueryParams{
		nonRepoIDLike: pq.StringArray(nonRepoIDLike),
		repoIDValues:  pq.StringArray(repoIDRaw),
	}, true
}

// loadDeferredScopedRelationshipFactsForPartition runs the self-exclusion query
// variant (listDeferredScopedRelationshipFactRecordsQuery) bounded to one
// (scope_id, generation_id) partition (issue #3710). The catalog-derived $1/$2
// parameters are shared across partitions; $3/$4 bind the partition. The DB
// excludes facts that only match because their own repo_id appears as an anchor,
// while still loading facts that reference ANOTHER repo's repo_id in their
// content (the repo_id arm is an EXISTS(unnest($2)) self-excluded substring test).
func loadDeferredScopedRelationshipFactsForPartition(
	ctx context.Context,
	queryer Queryer,
	params deferredScopedFactQueryParams,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(
		ctx,
		listDeferredScopedRelationshipFactRecordsQuery,
		params.nonRepoIDLike,
		params.repoIDValues,
		scopeID,
		generationID,
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
