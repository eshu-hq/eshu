// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

// Relationship table names: the closed `table` label set of the rows-swept
// counter and the orphan census gauge. resolved_relationships is absent on
// purpose: this slice never deletes a resolved row (its single registered
// deleter lands later, with the append-only contract change), so it has no
// label value yet.
const (
	EvidenceTable    = "relationship_evidence_facts"
	CandidatesTable  = "relationship_candidates"
	GenerationsTable = "relationship_generations"
)

// Skip reasons: the closed `reason` label set of the skipped-generations
// counter. A skipped generation keeps all its rows and is retried by a later
// pass.
const (
	// SkipLockBusy is a generation another session holds: its advisory
	// lock, or its relationship_generations row (an activation or a refinalize
	// in flight).
	SkipLockBusy = "lock_busy"
	// SkipLockTimeout is a batch that waited 500 ms for a lock (a concurrent
	// #10820 purge holds rows the batch needs) and rolled back. Every
	// generation of that batch is counted, because the error aborts the whole
	// batch transaction.
	SkipLockTimeout = "lock_timeout"
	// SkipIneligible is a generation that stopped satisfying Rule A between the
	// census and the batch (it has a scope_generations row again, a scope's
	// active pointer names it, or its generation row is active).
	SkipIneligible = "ineligible"
)

// Cap reasons: the closed `cap` label set of the capped-passes counter, and
// Result.Capped. A capped pass stopped with orphans left.
const (
	// CapRows is the row cap: the committed rows reached it.
	CapRows = "rows"
	// CapDeadline is the wall budget: the next batch would start past it.
	CapDeadline = "deadline"
	// CapList is the census list limit: the pass swept every generation it
	// listed, and the census counted more than it listed.
	CapList = "list"
)

// LockClass is the first key of the two-integer advisory lock that keeps two
// sweepers off the same generation: pg_try_advisory_xact_lock(LockClass,
// hashtext(generation_id)). It is the issue number, like the search index
// retention class 7427, and the module-wide scan in freshness/links
// (TestRelationshipRetentionLockClassDiffersFromTreeKeys) checks it against
// every other two-integer class in the tree. Two generations whose ids
// collide under hashtext only make one of them skip a pass as busy.
const LockClass = 10823

// BatchGenerations is the most generations one batch transaction removes.
// Measured on a seeded corpus (design for #10823): a 50-generation batch is
// about 1,700 rows, p50 8.7 ms and p95 91.7 ms, and writes about 1.43 MB of
// WAL (about 1.7 KB per row, scattered deletes write full page images). A
// batch is bounded in generations, not rows: a hub generation with many
// evidence rows makes it larger, so the row cap is checked between batches and
// the first batch always runs. There is no knob: a different size needs a new
// measurement in the evidence note.
const BatchGenerations = 50

// CensusListLimit is the most distinct orphan generations the census lists
// per table, and so the most one pass can sweep. The orphan gauge counts the
// distinct generations the census listed and saturates here: a value equal to
// CensusListLimit means "at least this many". The runbook sizes a larger
// backlog with a one-off read-only count instead.
const CensusListLimit = 5000

// DefaultRowCap is the rows one sweep pass deletes when the caller passes none:
// about 100,000 rows, or about 170 MB of WAL at the measured 1.7 KB per row,
// bounding a drain to roughly 1 GB of WAL per hour per reducer replica at the
// capped cadence. The first batch of a pass always runs.
const DefaultRowCap int64 = 100_000

// lockTimeoutSetting bounds how long one batch waits for a row lock another
// session holds. It sits below the server's default deadlock_timeout (1 s), so
// the sweeper, not the #10820 purge, gives way when their lock orders cross:
// the batch rolls back with 55P03, counts lock_timeout, and a later pass
// retries it. It must stay positive: lock_timeout = 0 disables the timeout.
const lockTimeoutSetting = "500ms"

// censusStatementTimeoutSetting bounds the census transaction. The steady
// state (no orphans) reads one index of every relationship table (0.7 to 1.0 s
// on 3.3 million evidence rows after VACUUM); a census that outlives this
// bound fails the pass and is retried at the next poll. It must stay positive.
const censusStatementTimeoutSetting = "60s"

// setLockTimeoutQuery and setStatementTimeoutQuery are SET LOCAL: they end with
// the transaction and never leak onto the pooled connection. $1 is the setting.
const (
	setLockTimeoutQuery      = `SELECT set_config('lock_timeout', $1, true)`
	setStatementTimeoutQuery = `SELECT set_config('statement_timeout', $1, true)`
)

// tryLockQuery takes the per-generation advisory lock for each generation in
// $1 inside the batch transaction and reports, per id, whether it got it. A try
// lock never waits, so the order it visits the ids in cannot form a wait cycle.
// The select-list form (not a WHERE) keeps the busy ids visible so the pass can
// count them exactly. The lock class is the integer literal the module-wide
// scan reads; do not build it with fmt or a parameter.
const tryLockQuery = `-- relationship retention: try-lock generations
SELECT c.generation_id,
       pg_try_advisory_xact_lock(10823, hashtext(c.generation_id)) AS locked
FROM unnest($1::text[]) AS c(generation_id)
`

// classifyQuery decides, in one snapshot, which of the try-locked generations
// in $1 the batch may delete, and locks their generation rows. For each id it
// returns whether every Rule A predicate holds and whether the batch claimed it:
// claimed means the id's relationship_generations row is now locked FOR UPDATE
// SKIP LOCKED and not active, or the id has no generation row at all (rows
// written but never resolved: the no_rg branch, which has nothing to lock).
//
// The status test sits on the locked relation itself, so under Read Committed
// the row lock re-evaluates it against the latest committed version: an
// activation that committed after the snapshot makes the row drop out instead
// of being deleted. SKIP LOCKED leaves a row another session holds (an
// activation or a refinalize in flight) for a later pass. Only the claimed ids
// feed the deletes.
const classifyQuery = `-- relationship retention: classify and lock generations
WITH wanted AS (
    SELECT w.generation_id FROM unnest($1::text[]) AS w(generation_id)
),
candidate AS (
    SELECT w.generation_id
    FROM wanted w
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = w.generation_id)
      AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = w.generation_id)
      AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = w.generation_id AND rg.status = 'active')
),
locked AS MATERIALIZED (
    SELECT rg.generation_id
    FROM relationship_generations rg
    JOIN candidate c ON c.generation_id = rg.generation_id
    WHERE rg.status <> 'active'
    ORDER BY rg.generation_id COLLATE "C"
    FOR UPDATE OF rg SKIP LOCKED
),
no_rg AS (
    SELECT c.generation_id
    FROM candidate c
    WHERE NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = c.generation_id)
)
SELECT w.generation_id,
       c.generation_id IS NOT NULL AS predicates_hold,
       (l.generation_id IS NOT NULL OR n.generation_id IS NOT NULL) AS claimed
FROM wanted w
LEFT JOIN candidate c ON c.generation_id = w.generation_id
LEFT JOIN locked l ON l.generation_id = w.generation_id
LEFT JOIN no_rg n ON n.generation_id = w.generation_id
`

// The three deletes run in this order inside the batch transaction: evidence,
// candidates, the generation row last. The #10820 purge deletes the same tables
// in the same order, and the generation row is the only one the batch locked
// before them, so it is the single place the two lock orders cross (the
// 500 ms lock_timeout breaks that cycle).
//
// Each delete re-asserts all three Rule A predicates in its own WHERE. $1 is a
// narrowing list, never the authority: a wrong array deletes nothing it should
// not, because every row is checked against scope_generations,
// ingestion_scopes.active_generation_id and the generation row's status in the
// delete's own snapshot. Nothing here names resolved_relationships.
const deleteEvidenceQuery = `-- relationship retention: delete evidence
DELETE FROM relationship_evidence_facts e
WHERE e.generation_id = ANY($1::text[])
  AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = e.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = e.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = e.generation_id AND rg.status = 'active')
`

const deleteCandidatesQuery = `-- relationship retention: delete candidates
DELETE FROM relationship_candidates c
WHERE c.generation_id = ANY($1::text[])
  AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = c.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = c.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = c.generation_id AND rg.status = 'active')
`

const deleteGenerationsQuery = `-- relationship retention: delete generations
DELETE FROM relationship_generations rg
WHERE rg.generation_id = ANY($1::text[])
  AND rg.status <> 'active'
  AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = rg.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = rg.generation_id)
`

// The census statements list distinct orphan generations, ordered and bounded
// by $1 (CensusListLimit+1, so saturation is detectable), one statement per
// table so the gauge can say where the backlog sits. They apply the same three
// predicates the deletes do, but the deletes stay the authority: a generation
// that stops being an orphan between the census and a batch is skipped as
// ineligible. The scope_generations anti-join runs under the DISTINCT so it
// stays one hash anti join over an index-only scan of the generation index
// (0.7 to 1.0 s on 3.3 million evidence rows after VACUUM, measured in the
// design); the two other predicates then run on distinct ids only. The
// visibility map matters: run VACUUM after bulk deletes or the index-only scan
// turns into heap fetches.
const censusEvidenceQuery = `-- relationship retention: census evidence
SELECT d.generation_id
FROM (
    SELECT DISTINCT e.generation_id
    FROM relationship_evidence_facts e
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = e.generation_id)
) d
WHERE NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = d.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = d.generation_id AND rg.status = 'active')
ORDER BY d.generation_id LIMIT $1
`

const censusCandidatesQuery = `-- relationship retention: census candidates
SELECT d.generation_id
FROM (
    SELECT DISTINCT c.generation_id
    FROM relationship_candidates c
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = c.generation_id)
) d
WHERE NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = d.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = d.generation_id AND rg.status = 'active')
ORDER BY d.generation_id LIMIT $1
`

const censusGenerationsQuery = `-- relationship retention: census generations
SELECT g.generation_id
FROM relationship_generations g
WHERE g.status <> 'active'
  AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = g.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = g.generation_id)
ORDER BY g.generation_id LIMIT $1
`
