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
// corpus-wide deferred backfill (issue #3659). It accepts six parameters:
//
//	$1 pq.StringArray — LIKE terms derived from non-repo_id aliases (name,
//	   slug tokens) and the unconditional ArgoCD over-select markers. A fact
//	   matching $1 carries a cross-repo reference that is NOT keyed on its own
//	   repo_id, so loading it can produce evidence and it is always loaded.
//
//	$2 pq.StringArray — raw lowercase repo_id values. The repo_id fallback arm
//	   uses EXISTS to load a fact only when its payload contains a catalog
//	   repo_id value that is not the row's own repo_id. The value stays raw so
//	   the exact self-exclusion comparison (catalog_repo_id.value <> own repo_id)
//	   is a whole string match; the substring LIKE escapes the value's LIKE
//	   metacharacters (\ % _) inline with the same ESCAPE '\' convention as $1
//	   so a repo_id containing one of those characters (or a trailing
//	   backslash) cannot become an accidental wildcard or a malformed escape
//	   sequence.
//
//	$3 string — the scope_id partition this run is bounded to (issue #3710).
//
//	$4 string — the generation_id partition this run is bounded to (issue #3710).
//
//	$5 sql.NullString (nullable text) — a POSIX ARE (Postgres `~` operator) alternation of
//	   every $2 repo_id value EXCEPT the partition's $6 performance-hint own
//	   repo_id, each value escaped so every ARE metacharacter is a literal
//	   (buildDeferredRepoIDRegex). NULL when no such alternation is buildable
//	   (empty catalog, or excluding $6 leaves nothing) — the query treats NULL
//	   as "the fast arm never fires for this partition", never as a match.
//
//	$6 string — a lowercase performance-hint "this partition's own repo_id",
//	   derived from scope_id with zero extra queries
//	   (deferredScopedFactOwnRepoIDFromScope): git-repository-scope:<repo_id>
//	   scopes resolve to <repo_id>; every other scope shape (GCP cloud-relationship
//	   scopes included) resolves to "". $6 is NOT a correctness input — see the
//	   "$5/$6 performance-hint" section below.
//
// Why EXISTS, not blind replace(): replace() corrupts overlap cases where the
// own repo_id is a prefix of another repo's repo_id (for example app vs
// app-config). EXISTS compares whole repo_id values, so the overlapping target
// still matches. The fallback boundary test is a plain LIKE substring rather
// than a boundary regex: LIKE widens the SQL to a provable superset and the
// in-memory catalogMatcher re-applies boundary-safe token matching, so a
// repo_id that is a substring of another (app vs app-config) is over-selected
// at SQL but still resolved to the correct whole-value match by the matcher.
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
// Payload hoist (issue #3624). The inner CTE computes lower(fact.payload::text)
// ONCE per row as payload_lower, and the row's repo_id lowercased and coalesced
// to the empty string when absent, ONCE per row as own_repo_id. Before
// this change, lower(fact.payload::text) was evaluated once for the $1 LIKE ANY
// test and AGAIN, once per unnest($2) catalog row, inside the EXISTS
// self-exclusion arm — an O(facts × catalog × payload_size)
// re-lowering of the same payload text for every candidate repo_id. Hoisting it
// into the CTE's SELECT list makes every arm below reference the already-computed
// column; lower(payload::text) is never recomputed. Proven on the live eshufull
// corpus: a 1,990-fact scope went from 149,525ms (pre-hoist) to 914ms with the
// hoist plus the $5/$6 fast arm below (163x), and a 24,802-fact scope completed in
// 13,864ms; both runs' fact_id sets are byte-identical to the pre-hoist shape
// (0/0 set-diff in both directions; see the differential proof test).
//
// $5/$6 performance-hint fast arm — correctness independent of $6. The repo_id
// predicate below is:
//
//	(own_repo_id = $6 AND payload_lower ~ $5)                                    -- fast arm
//	OR (own_repo_id <> $6 AND EXISTS(unnest($2) ... same fallback as before))     -- fallback arm
//
// own_repo_id stays a PER-ROW computed column (a single scope_id can carry rows
// whose payload->>'repo_id' differs from any single value derived from the
// scope_id, notably GCP cloud-relationship scopes, which have no repository
// fact and no single derivable repo_id at all). $6 is a hint about what "most"
// rows in this partition's own repo_id probably is, not an assertion that it IS:
//
//   - When $6 correctly predicts a row's own_repo_id, the fast arm fires and $5
//     (built by excluding exactly $6 from the $2 catalog) evaluates a single
//     regex match instead of a per-catalog-entry correlated EXISTS loop — the
//     source of the O(facts × catalog) cost this issue fixes.
//   - When $6 is wrong for a row (own_repo_id <> $6 — including every GCP
//     cloud-relationship row, since $6 is always "" for non-git-repository-scope
//     partitions and a GCP row's own repo_id is essentially never ""), the fast
//     arm's guard is false and that row falls through to the EXISTS fallback,
//     which is byte-for-byte the pre-hoist self-exclusion arm. The row is never
//     dropped and never wrongly matched merely because $6 was wrong.
//
// A wrong or absent $6 therefore only costs a fallback-arm evaluation for the
// affected rows (a performance cost bounded by the partition), never a
// correctness cost. This is why $6 can be derived for free from scope_id
// (deferredScopedFactOwnRepoIDFromScope: strip the "git-repository-scope:"
// prefix, or "" for any other scope shape) instead of requiring a discovery
// query: loadActiveRepositoryGenerations was considered and rejected, because it
// filters to fact_kind = 'repository' and drops every GCP cloud-relationship
// scope entirely, and a live-corpus mode()-in-discovery alternative was measured
// and also rejected (forces a heap scan plus an external-sort spill vs. the
// existing 225ms index-only discovery path). Proven on the live eshufull corpus
// (314,879 rows, all git-repository-scope): the scope_id-derived $6 equals the
// row's own_repo_id for 314,799 rows (99.975%); the remaining 80 rows are
// handled correctly by the fallback arm.
//
// $5 regex build (buildDeferredRepoIDRegex). $5 is built once per partition in
// Go from the shared $2 repoIDValues, excluding $6, with every POSIX ARE
// metacharacter (`\ . + * ? ( ) | [ ] { } ^ $`) escaped via regexp.QuoteMeta —
// verified directly against Postgres 18 to escape the identical character set
// Postgres's `~` operator requires escaped, unlike LIKE's `\ % _`. $5 is passed
// as NULL, not an empty-alternation "(?:)", when excluding $6 leaves zero
// values: "(?:)" is a zero-width match present in EVERY string under Postgres
// ARE (verified: `SELECT 'x' ~ '(?:)'` is true), so building it would silently
// turn "own_repo_id = $6 AND payload_lower ~ $5" into "own_repo_id = $6 AND
// true" — an over-selection bug, not merely a missed optimization. The query
// guards $5 IS NOT NULL before the `~` test so a NULL $5 makes the fast arm
// never fire for that partition, falling through to the fallback for every row
// (verified: a NULL-guarded `~` test with $5 = NULL returns false, never an
// error and never a spurious match).
//
// Performance shape (issue #3710, retained). The query is bound to one
// (scope_id, generation_id) partition via $3/$4 and selects from fact_records
// inside a `WITH matched_facts AS MATERIALIZED (...)` CTE. Two plan properties
// drove this shape (measured locally on a 7.3M-row fact_records under
// PostgreSQL 18; see the package README #3710 Performance Evidence for the
// EXPLAIN ANALYZE numbers):
//
//  1. Per-scope partition. The original query scanned every latest-generation
//     content/file/gcp fact once and evaluated the per-row self-exclusion arm
//     against the whole catalog: an O(facts × catalog) correlated scan over the
//     full corpus. The scope_generation predicate ($3/$4) bounds each run to one
//     scope's facts via fact_records_scope_generation_idx, turning the monolithic
//     scan into many small ones the caller fans out across the
//     deferred-maintenance worker pool. The planner bounds each per-scope query by
//     fact_records_scope_generation_idx and applies $1/$2/$5/$6 as filters on
//     that already-bounded set; no payload index is used.
//
//  2. MATERIALIZED candidate set. MATERIALIZED forces Postgres to build the
//     per-scope-bounded candidate set once (including the payload_lower and
//     own_repo_id hoist), then join that small result to the latest-generation
//     set, rather than re-deriving it on the inner side of a Nested Loop. The $1
//     LIKE ANY constant-pattern arm and the $5/$6 fast arm are then cheap
//     filters on the bounded candidate set, and the $2 EXISTS fallback arm — a
//     per-fact correlated loop that is un-indexable — runs only for rows the
//     fast arm did not already resolve, also bounded by the per-scope partition.
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
    FROM (
      SELECT
          inner_fact.*,
          lower(inner_fact.payload::text) AS payload_lower,
          lower(COALESCE(inner_fact.payload->>'repo_id', '')) AS own_repo_id
      FROM fact_records AS inner_fact
      WHERE inner_fact.scope_id = $3
        AND inner_fact.generation_id = $4
        AND inner_fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
    ) AS fact
    WHERE
        fact.payload_lower LIKE ANY($1)
        OR (fact.own_repo_id = $6 AND $5::text IS NOT NULL AND fact.payload_lower ~ $5)
        OR (
          fact.own_repo_id <> $6
          AND EXISTS (
            SELECT 1
            FROM unnest($2::text[]) AS catalog_repo_id(value)
            WHERE catalog_repo_id.value <> fact.own_repo_id
              AND fact.payload_lower LIKE
                '%' ||
                replace(replace(replace(catalog_repo_id.value, '\', '\\'), '%', '\%'), '_', '\_') ||
                '%' ESCAPE '\'
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
// parameters are shared across partitions; $3/$4 bind the partition. $6 is the
// scope_id-derived own-repo_id performance hint (deferredScopedFactOwnRepoIDFromScope)
// and $5 is the regex built by excluding $6 from params.repoIDValues
// (buildDeferredRepoIDRegex); $5 is passed as a NULL sql.NullString when no
// usable alternation exists, which safely disables the fast arm for this
// partition (see the query's doc comment for why an absent/wrong $6 only costs
// performance, never correctness). The DB excludes facts that only match
// because their own repo_id appears as an anchor, while still loading facts
// that reference ANOTHER repo's repo_id in their content (the fallback repo_id
// arm is an EXISTS(unnest($2)) self-excluded substring test, byte-identical to
// the pre-hoist shape).
func loadDeferredScopedRelationshipFactsForPartition(
	ctx context.Context,
	queryer Queryer,
	params deferredScopedFactQueryParams,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	ownRepoID := deferredScopedFactOwnRepoIDFromScope(scopeID)
	regex, ok := buildDeferredRepoIDRegex([]string(params.repoIDValues), ownRepoID)
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
