// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

// BatchRows is the rows one pre-delete batch transaction removes from one
// table. Measured for the shipped terms statement on PostgreSQL 18.6 under a
// forced generic plan (#7427 evidence note): a 20,000-row batch read
// 80,179-80,688 shared buffers (about 4 per row: 1 for the keyset page, 3 for
// the ctid delete), wrote 20,000 WAL records (1.08 MB, up to 5.2 MB when the
// first touch after a checkpoint writes full-page images), and ran 36-41 ms
// under EXPLAIN ANALYZE; a late batch cost the same. In a
// committed loop over a 400,000-row generation the batches ran p50 28 ms. One
// batch therefore holds its row locks and the per-generation advisory lock
// for tens of milliseconds. There is no knob: a different size needs a new
// measurement.
const BatchRows = 20000

// LockClass is the first key of the two-integer advisory lock that keeps a
// pre-delete batch and a retention prune of the same generation apart:
// pg_try_advisory_xact_lock(LockClass, hashtext(generation_id)). It is the
// issue number, like the changed-since slot class 7127, and the module-wide
// scan in freshness/links (TestSearchIndexRetentionLockClassDiffersFromTreeKeys)
// checks it against every other two-integer class in the tree. Two
// generations whose ids collide under hashtext only make one of them skip a
// pass as busy; they never share rows.
const LockClass = 7427

// lockTimeoutSetting bounds how long one batch waits for a row lock another
// session holds. It sits below the server's default deadlock_timeout (1 s), so
// the batch, not a writer, gives way: the generation stays undrained for this
// pass and the next pass retries it.
const lockTimeoutSetting = "500ms"

// setLockTimeoutQuery is SET LOCAL lock_timeout: it ends with the batch
// transaction and never leaks onto the pooled connection.
const setLockTimeoutQuery = `SELECT set_config('lock_timeout', $1, true)`

// tryLockQuery takes the per-generation advisory lock without waiting. $1
// generation id.
const tryLockQuery = `SELECT pg_try_advisory_xact_lock(7427, hashtext($1::text))`

// tryLockGenerationsQuery takes the per-generation advisory lock for each
// generation in $1 inside the caller's transaction and returns the ids it
// got. A try lock never waits, so the order it visits the ids in cannot form
// a wait cycle.
const tryLockGenerationsQuery = `-- retention: search index generation try-lock
SELECT candidate.generation_id
FROM unnest($1::text[]) AS candidate(generation_id)
WHERE pg_try_advisory_xact_lock(7427, hashtext(candidate.generation_id))
`

// eligibleGenerationCTE re-reads, in the batch's own statement and snapshot,
// that the generation is still a terminal superseded generation, is not the
// scope's active generation, and has no claimed, running, or retrying work
// item. The live-work test matches the retention selection's live_work
// exactly (generation_id and status, no scope_id): fact_work_items carries
// no (scope_id, generation_id) foreign key, so a work row's scope_id is not
// guaranteed to equal its generation's. When it returns no row the batch
// deletes nothing. $1 scope id, $2 generation id.
const eligibleGenerationCTE = `eligible AS MATERIALIZED (
    SELECT 1 AS ok
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.generation_id = $2
      AND generation.scope_id = $1
      AND generation.status = 'superseded'
      AND generation.superseded_at IS NOT NULL
      AND scope.active_generation_id IS DISTINCT FROM $2
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS work
          WHERE work.generation_id = $2
            AND work.status IN ('claimed', 'running', 'retrying')
      )
)`

// deleteTermsBatchQuery deletes one keyset page of a superseded generation's
// eshu_search_index_terms rows. The page reads the leaf's primary key
// (scope_id, generation_id, term_key, document_id) from the cursor ($3, $4)
// inclusive, in key order, and the DELETE removes exactly those rows by ctid.
// Matching by ctid is valid only because both sides carry scope_id = $1: hash
// partitioning on scope_id prunes the page and the DELETE to the same leaf,
// so every ctid names a row of that leaf, and the DELETE also re-checks
// scope_id and generation_id, so a ctid can never remove a row of another
// generation (the active generation's re-minted document ids included). The
// cursor is inclusive because the previous page's last key was deleted and
// committed, so it no longer matches; an inclusive start also lets a from-start
// page (”, ”) see every key. It returns whether the generation was still
// eligible, the page size, the rows deleted, and the page's last key. $1 scope
// id, $2 generation id, $3 term_key cursor, $4 document_id cursor, $5 page
// bound.
const deleteTermsBatchQuery = `-- retention: search index terms pre-delete batch
WITH ` + eligibleGenerationCTE + `,
` + termsBatchBody

// termsBatchBody is the terms batch after its eligibility CTE: the keyset
// page, the ctid DELETE, and the result row. The pre-delete and the orphan
// sweep share it, so both walk and delete the same way and differ only in
// which generations their eligibility CTE admits.
const termsBatchBody = `page AS MATERIALIZED (
    SELECT term.ctid AS row_ctid, term.term_key, term.document_id
    FROM eshu_search_index_terms AS term
    WHERE term.scope_id = $1
      AND term.generation_id = $2
      AND (term.term_key, term.document_id) >= ($3::text, $4::text)
      AND EXISTS (SELECT 1 FROM eligible)
    ORDER BY term.term_key, term.document_id
    LIMIT $5
),
deleted AS (
    DELETE FROM eshu_search_index_terms AS term
    WHERE term.scope_id = $1
      AND term.generation_id = $2
      AND term.ctid = ANY(ARRAY(SELECT page.row_ctid FROM page))
    RETURNING 1
),
last_key AS (
    SELECT page.term_key, page.document_id
    FROM page
    ORDER BY page.term_key DESC, page.document_id DESC
    LIMIT 1
)
SELECT EXISTS (SELECT 1 FROM eligible),
    (SELECT count(*) FROM page),
    (SELECT count(*) FROM deleted),
    COALESCE((SELECT last_key.term_key FROM last_key), ''),
    COALESCE((SELECT last_key.document_id FROM last_key), '')
`

// deleteDocumentsBatchQuery is deleteTermsBatchQuery for
// eshu_search_index_documents, keyed by its primary key (scope_id,
// generation_id, document_id). The table is not partitioned, so the ctid
// match needs no pruning argument; the DELETE still re-checks scope_id and
// generation_id. $1 scope id, $2 generation id, $3 document_id cursor, $4
// page bound.
const deleteDocumentsBatchQuery = `-- retention: search index documents pre-delete batch
WITH ` + eligibleGenerationCTE + `,
` + documentsBatchBody

// documentsBatchBody is the documents batch after its eligibility CTE,
// shared by the pre-delete and the orphan sweep like termsBatchBody.
const documentsBatchBody = `page AS MATERIALIZED (
    SELECT document.ctid AS row_ctid, document.document_id
    FROM eshu_search_index_documents AS document
    WHERE document.scope_id = $1
      AND document.generation_id = $2
      AND document.document_id >= $3::text
      AND EXISTS (SELECT 1 FROM eligible)
    ORDER BY document.document_id
    LIMIT $4
),
deleted AS (
    DELETE FROM eshu_search_index_documents AS document
    WHERE document.scope_id = $1
      AND document.generation_id = $2
      AND document.ctid = ANY(ARRAY(SELECT page.row_ctid FROM page))
    RETURNING 1
),
last_key AS (
    SELECT page.document_id
    FROM page
    ORDER BY page.document_id DESC
    LIMIT 1
)
SELECT EXISTS (SELECT 1 FROM eligible),
    (SELECT count(*) FROM page),
    (SELECT count(*) FROM deleted),
    COALESCE((SELECT last_key.document_id FROM last_key), ''),
    ''::text
`

// orphanGenerationCTE admits, in the batch's own statement and snapshot, only
// a generation whose (scope_id, generation_id) pair is absent from
// scope_generations and that the scope's active pointer does not name (#7427
// PR2). Readers join ingestion_scopes.active_generation_id, so such rows are
// read-invisible; the pointer test keeps that true even for a dangling
// pointer, which has no foreign key. A pair that exists in any status
// (pending, active, superseded, failed) is ineligible, so the sweep never
// deletes a row of an existing generation. $1 scope id, $2 generation id.
const orphanGenerationCTE = `eligible AS MATERIALIZED (
    SELECT 1 AS ok
    WHERE NOT EXISTS (
        SELECT 1
        FROM scope_generations AS generation
        WHERE generation.generation_id = $2
          AND generation.scope_id = $1
    )
      AND NOT EXISTS (
        SELECT 1
        FROM ingestion_scopes AS scope
        WHERE scope.scope_id = $1
          AND scope.active_generation_id = $2
    )
)`

// sweepTermsBatchQuery deletes one keyset page of an orphan generation's
// eshu_search_index_terms rows: deleteTermsBatchQuery with the orphan
// eligibility CTE. Same arguments and result row.
const sweepTermsBatchQuery = `-- retention: search index terms orphan sweep batch
WITH ` + orphanGenerationCTE + `,
` + termsBatchBody

// sweepDocumentsBatchQuery is sweepTermsBatchQuery for
// eshu_search_index_documents.
const sweepDocumentsBatchQuery = `-- retention: search index documents orphan sweep batch
WITH ` + orphanGenerationCTE + `,
` + documentsBatchBody

// SupersededSweepCandidateLimit is the most superseded generations one
// #8231 sweep lists. The sweep still counts every listed generation it does
// not drain as unswept, so a larger backlog drains over later sweeps. There
// is no knob: the pass budget already bounds the work.
const SupersededSweepCandidateLimit = 1000

// supersededEligibleCTE mirrors eligibleGenerationCTE and adds the #8231
// fail-closed line: the scope's active pointer must be NOT NULL. Without it
// a NULL pointer reads as "distinct from" every generation id and the batch
// would delete a generation no active-generation reader can be distinguished
// from. A missing generation row still yields no eligible row, so the batch
// deletes nothing. $1 scope id, $2 generation id.
const supersededEligibleCTE = `eligible AS MATERIALIZED (
    SELECT 1 AS ok
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.generation_id = $2
      AND generation.scope_id = $1
      AND generation.status = 'superseded'
      AND generation.superseded_at IS NOT NULL
      AND scope.active_generation_id IS NOT NULL
      AND scope.active_generation_id IS DISTINCT FROM $2
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS work
          WHERE work.generation_id = $2
            AND work.status IN ('claimed', 'running', 'retrying')
      )
)`

// sweepSupersededTermsBatchQuery deletes one keyset page of a superseded
// generation's eshu_search_index_terms rows: deleteTermsBatchQuery with the
// fail-closed #8231 eligibility CTE. Same arguments and result row.
const sweepSupersededTermsBatchQuery = `-- retention: search index superseded terms sweep batch
WITH ` + supersededEligibleCTE + `,
` + termsBatchBody

// sweepSupersededDocumentsBatchQuery is sweepSupersededTermsBatchQuery for
// eshu_search_index_documents.
const sweepSupersededDocumentsBatchQuery = `-- retention: search index superseded documents sweep batch
WITH ` + supersededEligibleCTE + `,
` + documentsBatchBody

// deleteSupersededStatsBatchQuery deletes one swept generation's single
// eshu_search_index_stats row under the same fail-closed eligibility. It
// runs after the generation's terms and documents drain, in its own batch
// transaction. $1 scope id, $2 generation id.
const deleteSupersededStatsBatchQuery = `-- retention: search index superseded stats sweep batch
WITH ` + supersededEligibleCTE + `,
deleted AS (
    DELETE FROM eshu_search_index_stats AS stats
    WHERE stats.scope_id = $1
      AND stats.generation_id = $2
      AND EXISTS (SELECT 1 FROM eligible)
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM eligible),
    (SELECT count(*) FROM deleted)
`

// supersededSweepCandidatesQuery lists the superseded generations a #8231
// sweep may drain, oldest first: terminal superseded rows the scope's live
// (NOT NULL) active pointer does not name, with no claimed, running, or
// retrying work item, that still hold search index rows in any of the three
// tables. A generation whose facts stay held but whose index rows are gone
// is not listed. $1 listing cap.
const supersededSweepCandidatesQuery = `-- retention: search index superseded sweep candidates
SELECT generation.scope_id, generation.generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.status = 'superseded'
  AND generation.superseded_at IS NOT NULL
  AND scope.active_generation_id IS NOT NULL
  AND scope.active_generation_id IS DISTINCT FROM generation.generation_id
  AND NOT EXISTS (
      SELECT 1
      FROM fact_work_items AS work
      WHERE work.generation_id = generation.generation_id
        AND work.status IN ('claimed', 'running', 'retrying')
  )
  AND (
      EXISTS (
          SELECT 1
          FROM eshu_search_index_terms AS term
          WHERE term.scope_id = generation.scope_id
            AND term.generation_id = generation.generation_id
      )
      OR EXISTS (
          SELECT 1
          FROM eshu_search_index_documents AS document
          WHERE document.scope_id = generation.scope_id
            AND document.generation_id = generation.generation_id
      )
      OR EXISTS (
          SELECT 1
          FROM eshu_search_index_stats AS stats
          WHERE stats.scope_id = generation.scope_id
            AND stats.generation_id = generation.generation_id
      )
  )
ORDER BY generation.superseded_at, generation.generation_id
LIMIT $1
`

// termsLeavesQuery lists the leaf partitions of eshu_search_index_terms as
// regclass text, which PostgreSQL quotes and schema-qualifies as the
// search_path needs, so the census can name each leaf safely. It returns no
// row for an install whose terms table is not partitioned.
const termsLeavesQuery = `-- retention: search index orphan census leaves
SELECT child.oid::regclass::text
FROM pg_inherits AS inheritance
JOIN pg_class AS child ON child.oid = inheritance.inhrelid
WHERE inheritance.inhparent = to_regclass('eshu_search_index_terms')
ORDER BY 1
`

// censusQuery enumerates the distinct (scope_id, generation_id) pairs of one
// relation (a terms leaf or eshu_search_index_documents) with a loose index
// scan over its primary-key prefix: each recursive step seeks to the first
// key after the previous pair, so the walk costs one index descent per pair
// plus any dead index entries it steps over, not a pass over every row. It
// returns the pairs walked, the pairs absent from scope_generations (the
// orphans), and at most $1 of those orphans, one per row (a NULL pair when
// there is none). relation must be a regclass text from termsLeavesQuery or a
// fixed table name; it is never caller input.
func censusQuery(relation string) string {
	return `-- retention: search index orphan census
WITH RECURSIVE pairs AS (
    (
        SELECT relation.scope_id, relation.generation_id
        FROM ` + relation + ` AS relation
        ORDER BY relation.scope_id, relation.generation_id
        LIMIT 1
    )
    UNION ALL
    SELECT following.scope_id, following.generation_id
    FROM pairs
    CROSS JOIN LATERAL (
        SELECT relation.scope_id, relation.generation_id
        FROM ` + relation + ` AS relation
        WHERE (relation.scope_id, relation.generation_id) > (pairs.scope_id, pairs.generation_id)
        ORDER BY relation.scope_id, relation.generation_id
        LIMIT 1
    ) AS following
),
orphans AS MATERIALIZED (
    SELECT pairs.scope_id, pairs.generation_id
    FROM pairs
    WHERE NOT EXISTS (
        SELECT 1
        FROM scope_generations AS generation
        WHERE generation.generation_id = pairs.generation_id
          AND generation.scope_id = pairs.scope_id
    )
)
SELECT (SELECT count(*) FROM pairs),
    (SELECT count(*) FROM orphans),
    listed.scope_id,
    listed.generation_id
FROM (SELECT 1) AS one
LEFT JOIN LATERAL (
    SELECT orphans.scope_id, orphans.generation_id
    FROM orphans
    ORDER BY orphans.scope_id, orphans.generation_id
    LIMIT $1
) AS listed ON true
`
}
