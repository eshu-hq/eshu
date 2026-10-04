// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

// CrossRepoDeadCodeCoverageGapCap caps how many incomplete consumer
// repositories one coverage answer names. The statement binds the cap plus one
// so a cut list is flagged, not mistaken for the whole set. The answer is
// "incomplete" however many repositories are missing, so the cap only bounds
// the detail an operator reads. Exported for ContentReader's coverage read in
// package query, which binds it as the statements' limit.
const CrossRepoDeadCodeCoverageGapCap = 25

// CrossRepoDeadCodeNamedConsumerCoverageQuery answers, for a list of consumer
// repositories, which are not proven complete (#7547). $1 is the list as one
// text[], $2 whether a listed repository with no active repository scope counts
// as incomplete, $3 the current verdict schema epoch (the caller binds
// reachabilitystore.CodeReachabilityVerdictSchemaEpoch), and $4 the row cap plus
// its sentinel.
//
// Universe: a repository CAN be a consumer only when it has code edges, so a
// scope is a gap only when its active generation has a shared_projection_intents
// row in the code_calls or inheritance_edges domain, reached through
// shared_projection_acceptance for that generation. Completed or still pending
// both count, but only to decide whether a missing or truncated watermark is a
// gap: a pending intent beside an existing truncated = false watermark is not one
// (a stale snapshot reads complete until the reducer rebuilds it). A repository with no
// such intent (docs, IaC) is complete without a watermark. A zero-root
// repository WITH intents is not excluded: its edges can still sit on a chain
// from a rooted repository to the producer symbol, and its truncated watermark
// is a gap.
//
// A scope with an active generation and such intents is a gap when it has no
// code_reachability_repository_watermarks row for that generation, or the row is
// truncated, or its verdict_schema_epoch is below $3 (the snapshot was built
// under older verdict semantics, so its truncated bit and rows may be wrong). A repository with several scopes is a gap when any one is. A
// repository the request named that has no active repository scope at all is a
// gap when $2 is true.
//
// Each gap row also says why and which snapshot is expected (#7547): state is
// no_snapshot_yet (no watermark), truncated (current-epoch watermark that
// cannot prove absence) or older_epoch (watermark below $3), tested in that
// order so a truncated bit outranks the epoch test; generation_id is the
// scope's active generation. A listed repository with no active scope reports
// 'no_active_scope' and a NULL generation_id. Both come from the join and
// columns the statement already reads, so the plan class is unchanged. A
// repository with several gap scopes yields one row (DISTINCT ON): a truncated
// scope first, because a refresh is not expected to close the repository while one stands,
// then the lowest generation id. The caller applies the same order to the
// all-repositories rows.
//
// Shape: the repository scopes the list names are found in ONE pass over the
// active-generation scopes with a hashed `source_key = ANY($1)` test, never by
// joining the list to the scopes row by row -- the planner chose a nested loop
// for that join, O(list x scopes): 1.5 M comparisons and 73 ms for 500 ids and
// 9 M comparisons and 520 ms for 3,000 on a 3,010-scope fixture. A listed
// repository with no active scope is found by a hashed NOT IN against that same
// set, and only when $2 asks for it.
//
// The intent probe is a scalar subquery with LIMIT 1 inside a CASE that runs it
// only for a scope that already failed the watermark test. That shape is what
// keeps it a correlated probe: written as EXISTS, Postgres 18 hoists it into a
// hashed subplan that reads every code_calls and inheritance_edges intent in the
// database (observed in EXPLAIN on a fixture).
//
// Plan class, measured on the QA replica (PostgreSQL 18.3, 819-row
// ingestion_scopes, see docs/internal/evidence/7547-cross-repo-consumer-coverage.md):
// the scope side is a cheap sequential scan on the custom plan (the generic plan,
// from the sixth prepared execution on, used the partial index
// ingestion_scopes_active_generation_idx; 799 of 819 scopes qualify, so the index
// buys little); there is no source_key index. Each gap candidate adds a watermark primary-key probe and, for
// a missing or truncated one, an acceptance primary-key prefix probe
// (scope_id, acceptance_unit_id) plus shared_projection_intents_acceptance_lookup_idx.
// The cost does not grow with producer candidates and never reads
// code_reachability_rows. It runs once per request.
//
// Exported for ContentReader's coverage read in package query, which sits next
// to the root-declared receiver.
const CrossRepoDeadCodeNamedConsumerCoverageQuery = `
WITH matched AS MATERIALIZED (
  SELECT scope.scope_id, scope.source_key, scope.active_generation_id
  FROM ingestion_scopes AS scope
  JOIN scope_generations AS generation
    ON generation.generation_id = scope.active_generation_id
   AND generation.status = 'active'
  WHERE scope.scope_kind = 'repository'
    AND scope.active_generation_id IS NOT NULL
    AND scope.source_key = ANY($1::text[])
), gaps AS (
  SELECT scope.source_key AS repository_id,
         scope.active_generation_id AS generation_id,
         CASE WHEN watermark.scope_id IS NULL THEN 'no_snapshot_yet'
              WHEN watermark.truncated THEN 'truncated'
              WHEN watermark.verdict_schema_epoch < $3::integer THEN 'older_epoch'
         END AS state
  FROM matched AS scope
  LEFT JOIN code_reachability_repository_watermarks AS watermark
    ON watermark.scope_id = scope.scope_id
   AND watermark.generation_id = scope.active_generation_id
   AND watermark.repository_id = scope.source_key
  WHERE CASE WHEN watermark.scope_id IS NULL OR watermark.truncated
                  OR watermark.verdict_schema_epoch < $3::integer
             THEN COALESCE((SELECT true
              FROM shared_projection_acceptance AS acceptance
              JOIN shared_projection_intents AS intent
                ON intent.scope_id = acceptance.scope_id
               AND intent.acceptance_unit_id = acceptance.acceptance_unit_id
               AND intent.source_run_id = acceptance.source_run_id
               AND intent.generation_id = acceptance.generation_id
               AND intent.projection_domain IN ('code_calls', 'inheritance_edges')
              WHERE acceptance.scope_id = scope.scope_id
                AND acceptance.generation_id = scope.active_generation_id
                AND acceptance.acceptance_unit_id = scope.source_key
              LIMIT 1), false)
             ELSE false END
  UNION ALL
  SELECT requested.id, NULL::text, 'no_active_scope'
  FROM unnest($1::text[]) AS requested(id)
  WHERE $2::boolean
    AND requested.id NOT IN (SELECT source_key FROM matched)
)
SELECT DISTINCT ON (repository_id) repository_id, state, generation_id
FROM gaps
ORDER BY repository_id, (state = 'truncated') DESC, generation_id
LIMIT $4
`

// CrossRepoDeadCodeAllConsumerCoverageQuery is the same question for every
// repository scope that has an active generation, for an unscoped caller who
// named no consumer. $1 is the current verdict schema epoch and $2 the row cap
// plus its sentinel. It applies the same universe as the named statement: a
// missing, truncated or older-epoch watermark is a gap only for a scope whose
// active generation has a code_calls or inheritance_edges intent, probed the
// same way and for the same reason.
//
// Each row carries the same state and generation_id as the named statement's;
// a repository with several gap scopes can return several rows, and the caller
// keeps one by the named statement's rule (a truncated scope first, then the
// lowest generation id).
//
// There is no ORDER BY, so the LIMIT stops the scan at the first cap-plus-one
// gaps; a fully covered corpus reads every repository scope once, which is the
// bound, and the response carries no count of the repositories it checked
// because that count would force the full scan. Rows are not ordered in SQL, so
// the caller sorts and dedupes the ones it keeps. The plan is unmeasured at QA
// scale; see the named statement.
//
// Exported for ContentReader's coverage read in package query.
const CrossRepoDeadCodeAllConsumerCoverageQuery = `
SELECT scope.source_key AS repository_id,
       CASE WHEN watermark.scope_id IS NULL THEN 'no_snapshot_yet'
            WHEN watermark.truncated THEN 'truncated'
            WHEN watermark.verdict_schema_epoch < $1::integer THEN 'older_epoch'
       END AS state,
       scope.active_generation_id AS generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.generation_id = scope.active_generation_id
 AND generation.status = 'active'
LEFT JOIN code_reachability_repository_watermarks AS watermark
  ON watermark.scope_id = scope.scope_id
 AND watermark.generation_id = scope.active_generation_id
 AND watermark.repository_id = scope.source_key
WHERE scope.scope_kind = 'repository'
  AND scope.active_generation_id IS NOT NULL
  AND CASE WHEN watermark.scope_id IS NULL OR watermark.truncated
                OR watermark.verdict_schema_epoch < $1::integer
           THEN COALESCE((SELECT true
            FROM shared_projection_acceptance AS acceptance
            JOIN shared_projection_intents AS intent
              ON intent.scope_id = acceptance.scope_id
             AND intent.acceptance_unit_id = acceptance.acceptance_unit_id
             AND intent.source_run_id = acceptance.source_run_id
             AND intent.generation_id = acceptance.generation_id
             AND intent.projection_domain IN ('code_calls', 'inheritance_edges')
            WHERE acceptance.scope_id = scope.scope_id
              AND acceptance.generation_id = scope.active_generation_id
              AND acceptance.acceptance_unit_id = scope.source_key
            LIMIT 1), false)
           ELSE false END
LIMIT $2
`
