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
// counter. A skipped generation keeps its rows (all of them, or the ones a
// batch could not take) and is retried by a later pass.
const (
	// SkipLockBusy is a generation another session holds, which the batch
	// leaves instead of waiting for: its advisory lock, its
	// relationship_generations row (an activation or a refinalize in flight, or a
	// row a writer created after classification, which had nothing to lock), or
	// evidence or candidate rows (a #10820 purge in flight, a writer). The batch
	// takes row locks only with SKIP LOCKED, so it never waits for any of them. A
	// generation whose row an activation turned active between classification's
	// snapshot and its row lock is counted here too, although it is no longer an
	// orphan: the lock drops the row and the statement cannot tell that from a row
	// it could not lock. A later pass's census does not list it.
	SkipLockBusy = "lock_busy"
	// SkipLockTimeout is a batch that waited 500 ms for a lock it cannot skip,
	// a relation-level lock (a DDL statement or VACUUM FULL holding a table), and
	// rolled back. Every generation of that batch is counted, because the error
	// aborts the whole batch transaction. A row lock never causes it.
	SkipLockTimeout = "lock_timeout"
	// SkipIneligible is a generation that stopped satisfying Rule A between the
	// census and the batch (it has a scope_generations row again, a scope's
	// active pointer names it, or its generation row was already active when
	// classification took its snapshot; a row that turns active only after that
	// snapshot is counted lock_busy, above).
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

// BatchGenerations is the most generations one batch transaction removes; the
// rows of a batch are bounded separately, by BatchRows. Measured on a local
// PostgreSQL 18 (docs/internal/evidence/10823-relationship-retention.md): a batch
// of 50 generations of 53 rows is about 2,650 rows. On a calm host (load average
// about 20) it commits in 52 to 57 ms at p50 and 109 to 127 ms at p95 when a
// generation's rows sit together. Over rows scattered at random, in a busier
// window (load average 24 to 50), the first pass took 435 ms at p50 and 1.06 s at
// p95, and a window at load averages of 55 to 85 ran the same statements 27 to 63
// times slower, so these are best-case figures. The
// runs set fsync=off and synchronous_commit=off, which remove the commit's wait for
// a WAL flush: they change how long a commit takes, not how much WAL a batch
// writes, and a production commit also waits for the flush. WAL depends on how the
// rows sit in the heap: about 0.34 KB per row when a generation's rows are
// together, and 1.3 KB per row on the first pass over rows scattered at random
// right after a checkpoint (S1). The 1.7 KB scattered-across-a-large-table and
// 4.3 KB right-after-a-checkpoint figures (S5) were measured before the row lock
// record added about 55 B per row and were not repeated. There is no knob: a
// different size needs a new measurement in the evidence note.
const BatchGenerations = 50

// BatchRows is the most rows each of a batch's two picking deletes (evidence,
// candidates) takes, so a batch deletes at most 2*BatchRows+BatchGenerations rows
// however large a generation is. A generation with more rows than that is drained
// by successive batches inside the same pass, each its own transaction. Without it
// a batch was bounded in generations only: the probe's hub generation (120,000
// evidence and 2,000 candidate rows) ran as one 124,598-row batch with its 49
// neighbours and held every row lock until the batch committed (the evidence note
// records how long). A #10820 purge of that repository waits for the batch, so the
// bound is on how long it can wait.
const BatchRows = 10_000

// CensusListLimit is the most distinct orphan generations the census lists
// per table, and so the most one pass can sweep. The orphan gauge counts the
// distinct generations the census listed and saturates here: a value equal to
// CensusListLimit means "at least this many". The runbook sizes a larger
// backlog with a one-off read-only count instead.
const CensusListLimit = 5000

// DefaultRowCap is the rows one sweep pass deletes when the caller passes none:
// about 100,000 rows. At the 1.7 KB per row planning figure for rows scattered
// across a large table that is about 170 MB of WAL per pass (measured before the
// row lock record added about 55 B per row, 3%). A capped pass repeats after a
// sixth of the poll interval, so one replica that always finds a backlog writes
// about 1 GB of WAL per hour at the default 1 h poll. More replicas raise that
// rate, not the WAL a drain writes in total: they share the backlog through the
// advisory try locks. Rows that sit together cost about a fifth of the planning
// figure, and a pass right after a checkpoint up to 2.5 times more. The first batch
// of a pass always runs.
const DefaultRowCap int64 = 100_000

// lockTimeoutSetting bounds how long one batch waits for a relation-level lock,
// the one wait it cannot skip: a DDL statement or VACUUM FULL holding ACCESS
// EXCLUSIVE on a relationship table. The batch never waits for a row lock (its
// row locks are all SKIP LOCKED), so this is a backstop that keeps a stuck table
// lock from holding a reducer connection, not a way to break a lock cycle with
// the #10820 purge: a lock timeout only ends the waiter that carries it, and the
// deadlock check of the other waiter can fire first. The batch rolls back with
// 55P03, counts lock_timeout, and a later pass retries it. It must stay
// positive: lock_timeout = 0 disables the timeout.
const lockTimeoutSetting = "500ms"

// censusStatementTimeoutSetting bounds the census transaction. The census reads
// every generation id of each relationship table once: 0.2 to 0.3 s warm (0.5 to
// 1.2 s on a first pass) with no orphans on 3.6 million evidence rows after
// VACUUM, and 2.4 to 8.5 s with 2 million orphan rows among 5.6 million, the
// slow end when the planner read the evidence table with a parallel sequential
// scan (docs/internal/evidence/10823-relationship-retention.md). A census that
// outlives this bound fails the pass and is retried at the next poll. The bound
// has about seven times the headroom of the slowest first run (8.5 s). Re-runs
// on a busier host were slower. With orphans (1.1 million evidence rows, together
// layout) a pass took 209 to 531 ms against 162 to 235 ms, and with none (0.72
// million rows) 97 to 823 ms against 49 to 55 ms. Whether load can push the 8.5 s
// case past this bound was not measured. It must stay positive.
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
// candidates, the generation row last. That is the order the #10820 purge
// deletes the same tables in, and it is only the delete order: the lock orders
// still differ, because the batch locks the generation row first (in
// classification) and the purge reaches it last. They are safe together because
// the batch never waits for a row lock, so it cannot be a link in a wait cycle.
// Classification takes the generation rows with SKIP LOCKED; each of the three
// deletes selects the rows it will delete in a MATERIALIZED CTE, FOR UPDATE SKIP
// LOCKED, and deletes exactly those ids in the same statement, so a row a purge or
// a writer holds is left in place, not waited on. The generation row delete takes
// its own lock because classification cannot lock a row that did not exist yet.
// The only wait edge left is the purge (or a writer) waiting for the batch, which
// ends when the batch commits or rolls back.
//
// The two picking statements stop after $2 rows (BatchRows), counted over the rows
// they lock: a row another session holds is skipped and does not count against the
// limit. That bounds what one batch holds and how long a purge or a writer can wait
// for it, however large a generation is; a generation with more rows than that is
// finished by the batches that follow (see the settle read).
//
// Each delete carries all three Rule A predicates itself, in the statement that
// takes the rows. $1 is a narrowing list, never the authority: a wrong array
// deletes nothing it should not, because every row is checked against
// scope_generations, ingestion_scopes.active_generation_id and the generation
// row's status in the delete's own snapshot. Nothing here names
// resolved_relationships.
const deleteEvidenceQuery = `-- relationship retention: delete evidence
WITH picked AS MATERIALIZED (
    SELECT e.evidence_id
    FROM relationship_evidence_facts e
    WHERE e.generation_id = ANY($1::text[])
      AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = e.generation_id)
      AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = e.generation_id)
      AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = e.generation_id AND rg.status = 'active')
    LIMIT $2
    FOR UPDATE OF e SKIP LOCKED
)
DELETE FROM relationship_evidence_facts e
USING picked p
WHERE e.evidence_id = p.evidence_id
`

const deleteCandidatesQuery = `-- relationship retention: delete candidates
WITH picked AS MATERIALIZED (
    SELECT c.candidate_id
    FROM relationship_candidates c
    WHERE c.generation_id = ANY($1::text[])
      AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = c.generation_id)
      AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = c.generation_id)
      AND NOT EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = c.generation_id AND rg.status = 'active')
    LIMIT $2
    FOR UPDATE OF c SKIP LOCKED
)
DELETE FROM relationship_candidates c
USING picked p
WHERE c.candidate_id = p.candidate_id
`

// deleteGenerationsQuery deletes a generation row only when no evidence and no
// candidate row of it is left. It picks the row the way the two deletes above pick
// theirs, in a MATERIALIZED CTE FOR UPDATE SKIP LOCKED, so it waits for no row
// lock. Classification holds the lock of every generation row that existed when it
// ran, but a generation with no generation row (the no_rg branch) was claimed
// without locking anything, and a writer can create that row, and hold it, before
// this statement runs. A plain DELETE would wait for that row until lock_timeout
// rolled the whole batch back (up to 50 generations counted lock_timeout), or close
// a lock cycle if the writer waited for a row the batch had deleted. This statement
// skips the row: the settle read still finds it and the batch counts the generation
// lock_busy for a later pass.
//
// The status test sits in the same SELECT as the lock, so under Read Committed the
// lock re-checks it on the row's newest version: a row that turned active after the
// statement's snapshot is not deleted. A purge or a writer that holds rows the two
// deletes skipped keeps them visible to the child tests here, so the generation row
// stays with them, and the batch reports the generation as left for a later pass.
const deleteGenerationsQuery = `-- relationship retention: delete generations
WITH picked AS MATERIALIZED (
    SELECT rg.generation_id
    FROM relationship_generations rg
    WHERE rg.generation_id = ANY($1::text[])
      AND rg.status <> 'active'
      AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = rg.generation_id)
      AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = rg.generation_id)
      AND NOT EXISTS (SELECT 1 FROM relationship_evidence_facts e WHERE e.generation_id = rg.generation_id)
      AND NOT EXISTS (SELECT 1 FROM relationship_candidates c WHERE c.generation_id = rg.generation_id)
    FOR UPDATE OF rg SKIP LOCKED
)
DELETE FROM relationship_generations rg
USING picked p
WHERE rg.generation_id = p.generation_id
`

// settleQuery runs after the three deletes, in the same transaction, and returns
// the claimed generations in $1 that still have a row in any of the three
// tables. The batch's own deletes are invisible to it; rows another session
// deleted but has not committed (a purge in flight), rows it holds, and rows a
// writer added are not. A generation it returns was not swept. If neither
// picking delete stopped at its row limit, another session holds what is left and
// the batch counts the generation as lock_busy for a later pass; if one did, the
// generation may just be larger than the batch and the pass runs it again. One
// that it does not return has nothing left, which includes a generation whose
// rows another session or replica removed after the census listed it.
const settleQuery = `-- relationship retention: settle claimed generations
SELECT c.generation_id
FROM unnest($1::text[]) AS c(generation_id)
WHERE EXISTS (SELECT 1 FROM relationship_evidence_facts e WHERE e.generation_id = c.generation_id)
   OR EXISTS (SELECT 1 FROM relationship_candidates k WHERE k.generation_id = c.generation_id)
   OR EXISTS (SELECT 1 FROM relationship_generations rg WHERE rg.generation_id = c.generation_id)
`

// The census statements list distinct orphan generations, ordered and bounded
// by $1 (CensusListLimit+1, so saturation is detectable), one statement per
// table so the gauge can say where the backlog sits. They apply the same three
// predicates the deletes do, but the deletes stay the authority: a generation
// that stops being an orphan between the census and a batch is skipped as
// ineligible. The scope_generations anti-join runs under the DISTINCT, so the
// two other predicates run on distinct ids only. The planner reads the
// generation ids with an index-only scan of the generation index while the
// visibility map is current and with a parallel sequential scan when it is not;
// both read each id once. Run VACUUM after bulk deletes to keep the first.
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
