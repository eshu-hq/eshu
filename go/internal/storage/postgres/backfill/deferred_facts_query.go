// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// ListDeferredScopedRelationshipFactRecordsQuery is the self-exclusion variant
// of ListOnboardedRepoScopedRelationshipFactRecordsQuery used exclusively by the
// corpus-wide deferred backfill (issue #3659). It accepts eight parameters:
//
//	$1 array.StringArray — LIKE terms derived from non-repo_id aliases (name,
//	   slug tokens) and the unconditional payload markers. This arm is
//	   intentionally guarded by DeferredRelationshipFamilyCandidatePredicateSQL:
//	   a content/file fact must first look like a relationship extractor family
//	   before the broad alias substring scan can select it. That preserves
//	   evidence-producing Terraform/Helm/ArgoCD/Docker/GitHub Actions/Ansible/
//	   Salt/GCP rows while preventing generic content partitions from being
//	   loaded only because they contain noisy short aliases such as "s3" or
//	   "robots".
//
//	$2 array.StringArray — raw lowercase repo_id values. The primary repo_id arm
//	   loads a fact only when its precomputed reference keys contain a catalog
//	   repo_id value that is not the row's own repo_id. Facts without precomputed
//	   reference keys retain the payload fallback. The values stay raw so both
//	   self-exclusion comparisons remain whole string matches.
//
//	$3 string — the scope_id partition this run is bounded to (issue #3710).
//
//	$4 string — the generation_id partition this run is bounded to (issue #3710).
//
//	$5 sql.NullString (nullable text) — a POSIX ARE (Postgres `~` operator) alternation of
//	   every $2 repo_id value EXCEPT the partition's $6 performance-hint own
//	   repo_id, each value escaped so every ARE metacharacter is a literal
//	   (BuildDeferredRepoIDRegex). NULL when no such alternation is buildable
//	   (empty catalog, or excluding $6 leaves nothing) — the query treats NULL
//	   as "the fast arm never fires for this partition", never as a match.
//
//	$6 string — a lowercase performance-hint "this partition's own repo_id",
//	   derived from scope_id with zero extra queries
//	   (scopestore.RepoIDFromScopeID): git-repository-scope:<repo_id>
//	   scopes resolve to <repo_id>; every other scope shape (GCP cloud-relationship
//	   scopes included) resolves to "". $6 is NOT a correctness input — see the
//	   "$5/$6 performance-hint" section below.
//
//	$7 array.StringArray — boundary-aware token keys corresponding 1:1 with $2.
//	   The precomputed relationship_reference_candidate_keys table stores each
//	   accepted content/file/GCP fact's payload as the same delimiter-wrapped token
//	   stream. Joining $7 against that stream replaces the steady-state
//	   facts × catalog payload predicate while preserving whole-token
//	   self-exclusion semantics.
//
//	$8 array.StringArray — raw alias anchors for the keyed arms (issue #10875).
//	   The same alias anchors $1 wraps in LIKE terms, without LIKE escaping,
//	   without %...% wrapping, and without the unconditional markers (the
//	   keyed marker arm reads the precomputed um_* bits instead). The keyed
//	   anchor arm tests each anchor with position(anchor in reference_key) > 0
//	   over version-1 sidecar rows whose family predicate is recomposed from
//	   the sidecar columns, so it never touches fact_records. An empty $8
//	   simply contributes no keyed anchor match; the keyed marker arm still
//	   applies, and facts without version-1 rows keep the legacy text arms
//	   verbatim (including the $5/$6 payload fallback).
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
// The per-commit scoped query (ListOnboardedRepoScopedRelationshipFactRecordsQuery)
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
//     arm's guard is false. A row with precomputed reference keys uses the
//     reference-key arm; a row without them falls through to the byte-identical
//     pre-hoist EXISTS fallback. The row is never dropped or wrongly matched
//     merely because $6 was wrong.
//
// A wrong or absent $6 therefore only costs a fallback-arm evaluation for the
// affected rows (a performance cost bounded by the partition), never a
// correctness cost. This is why $6 can be derived for free from scope_id
// (scopestore.RepoIDFromScopeID: strip scope.GitRepositoryScopePrefix,
// or "" for any other scope shape) instead of requiring a discovery
// query: loadActiveRepositoryGenerations was considered and rejected, because it
// filters to fact_kind = 'repository' and drops every GCP cloud-relationship
// scope entirely, and a live-corpus mode()-in-discovery alternative was measured
// and also rejected (forces a heap scan plus an external-sort spill vs. the
// existing 225ms index-only discovery path). Proven on the live eshufull corpus
// (314,879 rows, all git-repository-scope): the scope_id-derived $6 equals the
// row's own_repo_id for 314,799 rows (99.975%); the remaining 80 rows are
// handled correctly by the fallback arm.
//
// $5 regex build (BuildDeferredRepoIDRegex). $5 is built once per partition in
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
//     filters on the bounded candidate set. The $2 reference-key arm and its
//     no-reference-key compatibility fallback are also partition-bounded.
//
// Relationship-family partial index and reference narrowing (#5122). The source
// CTE applies DeferredRelationshipFamilyCandidatePredicateSQL directly to the
// base fact_records relation, exactly matching migration 059's partial-index
// predicate. PostgreSQL can therefore fetch the small extractor-family surface
// before detoasting generic content payloads. candidate_reference_keys then
// joins reference rows to those fact_ids before comparing catalog keys, avoiding
// the prior catalog-size × whole-partition reference-key cross-product. Both
// transformations are output-preserving: the same exact family predicate
// already guarded every payload-matching arm, and the reference CTE changes
// join order without changing its fact_id, scope, generation, or self-exclusion
// predicates.
// DeferredScopedLatestGenerationCTE is the partition-bounded latest-generation
// lookup for ListDeferredScopedRelationshipFactRecordsQuery (issue #8875
// round 2). The deferred fact load fans out one query per (scope_id,
// generation_id) partition and joins latest_generations only to confirm the
// partition's own generation is still latest, so the shared corpus-wide
// LatestGenerationCTE recomputed every scope's latest generation on every
// call: pg_stat_statements on the primary ranked the deferred scan
// fourth by total time (3,689,085 calls, mean 175.43ms, ~11 rows per call),
// and the read-only EXPLAIN shape showed the unbounded latest subquery
// dominating the estimated cost (Merge Join cost 499.51..1729.50 of 1746.66
// total) while scanning all 2,476 latest rows to keep at most one.
//
// The text is byte-identical to LatestGenerationCTE except for the added
// `WHERE generation.scope_id = $3` partition bound, which is output-preserving
// because the final join already requires latest.scope_id = fact.scope_id
// with fact bound to ($3, $4): latest rows for any other scope can never join.
// Measured on a disposable PostgreSQL 18 seeded to the production scale drivers
// (2,474 scopes, 15,892 generations, the v2 family partial index, 1029-term $1
// and 1024-value $2/$7 binds, ~3KB payloads) via EXPLAIN (ANALYZE, BUFFERS):
// a 135-row partition went from 246 to 12 shared-buffer hits (-95%) and 32.14
// to 26.97ms median (-16%) with byte-identical matched fact_id sets, while a
// 2000-row candidate-dominated partition was neutral (730 reads both shapes).
// TestDeferredScopedLatestLookupDerivesFromSharedCTE pins the lockstep so the
// bounded text cannot drift from the shared selection.
const DeferredScopedLatestGenerationCTE = `WITH latest_generations AS (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            generation.generation_id
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.scope_id = $3
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
)`

const ListDeferredScopedRelationshipFactRecordsQuery = DeferredScopedLatestGenerationCTE + `,
source_facts AS MATERIALIZED (
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
        fact.payload,
        lower(COALESCE(fact.payload->>'repo_id', '')) AS own_repo_id
    FROM fact_records AS fact
    WHERE fact.scope_id = $3
      AND fact.generation_id = $4
      AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND ` + DeferredRelationshipFamilyCandidatePredicateSQL + `
),
relationship_family_payload_facts AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.own_repo_id,
        lower(fact.payload::text) AS payload_lower
    FROM source_facts AS fact
    ` + DeferredRelationshipFamilyPayloadFactsFilterSQL + `
),
candidate_reference_keys AS MATERIALIZED (
    SELECT ref.fact_id, ref.source_repo_id, ref.reference_key
    FROM relationship_family_payload_facts AS fact
    JOIN relationship_reference_candidate_keys AS ref
      ON ref.fact_id = fact.fact_id
     AND ref.scope_id = $3
     AND ref.generation_id = $4
),
-- keyed_reference_keys holds this partition's version-1 sidecar rows
-- (issue #10875). Every column the keyed arms read is derived from the
-- fact's own envelope alone, so these arms never touch fact_records and
-- never detoast a payload.
keyed_reference_keys AS MATERIALIZED (
    SELECT
        ref.fact_id,
        ref.source_repo_id,
        ref.reference_key,
        ref.fact_kind,
        ref.artifact_type_norm,
        ref.path_norm,
        ref.content_argocd_kind,
        ref.content_salt_gitfs,
        ref.flux_git_nonempty,
        ref.um_kind_application,
        ref.um_artifact_argocd,
        ref.um_argocd_applications,
        ref.um_argocd_applicationsets,
        ref.um_flux_git_repositories
    FROM relationship_reference_candidate_keys AS ref
    WHERE ref.scope_id = $3
      AND ref.generation_id = $4
      AND ref.keys_version >= 1
),
keyed_anchor_matches AS MATERIALIZED (
    SELECT ref.fact_id
    FROM keyed_reference_keys AS ref
    WHERE ` + DeferredRelationshipFamilySidecarPredicateSQL + `
      AND EXISTS (
        SELECT 1
        FROM unnest($8::text[]) AS anchor(value)
        WHERE position(anchor.value in ref.reference_key) > 0
      )
),
keyed_marker_matches AS MATERIALIZED (
    SELECT ref.fact_id
    FROM keyed_reference_keys AS ref
    WHERE ` + DeferredRelationshipFamilySidecarPredicateSQL + `
      AND ` + DeferredRelationshipFamilySidecarMarkerArmSQL + `
),
matched_fact_ids AS MATERIALIZED (
        SELECT ref.fact_id
        FROM keyed_anchor_matches AS ref
        UNION
        SELECT ref.fact_id
        FROM keyed_marker_matches AS ref
        UNION
        SELECT fact.fact_id
        FROM relationship_family_payload_facts AS fact
        WHERE fact.payload_lower LIKE ANY($1)
        UNION
        SELECT ref.fact_id
        FROM candidate_reference_keys AS ref
        JOIN unnest($2::text[], $7::text[]) AS catalog_repo_id(value, reference_key)
          ON catalog_repo_id.value <> ref.source_repo_id
         AND position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0
        UNION
        SELECT fact.fact_id
        FROM relationship_family_payload_facts AS fact
        WHERE NOT EXISTS (
            SELECT 1
            FROM relationship_reference_candidate_keys AS ref
            WHERE ref.fact_id = fact.fact_id
              AND ref.scope_id = $3
              AND ref.generation_id = $4
          )
          AND NOT EXISTS (
            SELECT 1
            FROM keyed_reference_keys AS keyed
            WHERE keyed.fact_id = fact.fact_id
          )
          AND (
            (fact.own_repo_id = $6 AND $5::text IS NOT NULL AND fact.payload_lower ~ $5)
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
FROM source_facts AS fact
JOIN matched_fact_ids AS matched
  ON matched.fact_id = fact.fact_id
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`
