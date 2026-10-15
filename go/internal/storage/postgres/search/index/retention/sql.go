// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

// BatchRows is the rows one pre-delete batch transaction removes from one
// table. Measured on PostgreSQL 18.6 against a 4.1M-row terms leaf (#7427):
// the ctid-array keyset batch at 20,000 rows cost 1.12 us and 3.0 buffers per
// deleted row, 23 ms and 1.08 MB of WAL per batch, with zero full-page images,
// and the first and a late batch cost the same. One batch therefore holds its
// row locks and the per-generation advisory lock for tens of milliseconds, so
// a writer or a concurrent pass waits at most that long. There is no knob:
// a different size needs a new measurement.
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
// item. When it returns no row the batch deletes nothing. $1 scope id, $2
// generation id.
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
          WHERE work.scope_id = $1
            AND work.generation_id = $2
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
page AS MATERIALIZED (
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
page AS MATERIALIZED (
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
