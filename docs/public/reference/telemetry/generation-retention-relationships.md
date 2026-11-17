# Generation Retention Relationships

The reducer's generation-retention runner removes the relationship evidence of
generations nothing keeps alive (#10823). Relationship evidence
(`relationship_evidence_facts`), candidates (`relationship_candidates`) and
generation rows (`relationship_generations`) are written per generation and
were deleted only when a repository retired (the #10820 purge), so the rows of a
superseded generation grew with every re-index. Each pass takes a
read-only census of the orphans and, only when the sweep is enabled, deletes
them in small committed batches after the prune transaction has ended.

## What counts as an orphan

A relationship generation is removable when all three hold:

- no `scope_generations` row names it;
- its `relationship_generations` row is not `active` (or it has none);
- no `ingestion_scopes.active_generation_id` points at it.

Every delete statement re-checks all three in its own snapshot, so a
generation that stops being an orphan between the census and a batch is
skipped, never deleted. `resolved_relationships` is append-only and this sweep
does not touch it. A later change owns removing those rows.

**Served contract.** `GET /api/v0/evidence/relationships/{resolved_id}` joins
the generation row, so once the sweep removes a generation every `resolved_id`
it produced answers the existing `404` (`relationship evidence not found`),
the same as an id that never existed. That includes a `resolved_id` read from
a graph edge. The status and body do not change; re-read the repository or
service context for a current pointer.

## Flags

| Variable | Default | Effect |
|---|---|---|
| `ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ENABLED` | `false` | Opts into the deletes. With it off, every retention pass still takes the census and sets the orphan gauge, and sends no `DELETE`. An unset or unparsable value leaves deletes off. |
| `ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ROW_CAP` | `100000` | Rows one sweep pass deletes. The pass stops after the batch that crosses the cap. An unset, unparsable or non-positive value keeps the default. |

> Enablement is HELD per environment until the operator gate below is
> recorded. The default flips only after a recorded drain in a shared
> environment.

## Cadence and bounds

- The runner takes the census and, when enabled, sweeps at most once per
  retention poll interval (`1h` by default; the
  [Resolution Engine](../../services/resolution-engine.md) page lists the
  setting), after the search index sweeps. A failed pass is recorded and logged
  and does not fail the retention pass.
- A pass deletes at most the row cap, and stops after a quarter of the poll
  interval. A batch holds at most 50 generations and deletes at most 20,050
  rows (each of its two row-picking deletes stops at 10,000), so a generation
  larger than that is drained by successive batches inside the same pass, each
  its own transaction. The first batch always runs, so one pass can exceed the
  cap by up to one batch.
- After a pass that stopped on the row cap, the deadline or the census list
  limit, the runner re-runs after a sixth of the poll interval so a backlog
  drains in hours. It never re-runs early after a failed pass, a clean pass, or
  a pass that skipped a batch on `lock_timeout`, and any later pass clears the
  shortened cadence (a failed pass after a capped one waits the full interval).
  While the latest pass was capped, the same sixth also bounds the retention
  prune's own idle wait and its skipped-retry wait, so a relationship backlog
  makes the prune poll more often too; the search index sweeps keep their own
  once-per-poll due checks. A prune that fails waits the full poll interval,
  even straight after a capped relationship pass.
- The census lists at most 5,000 orphan generations per table; the rest are
  listed by later passes. The sweep re-lists from the lowest id each pass.
- The census reads every generation id of each relationship table once: 0.2 to
  0.3 s warm (0.5 to 1.2 s on a first pass) on 3.6 million evidence rows with no
  orphans, 2.4 to 8.5 s with 2 million orphan rows among 5.6 million, the slow
  end when the planner reads the evidence table with a parallel sequential scan.
  Those are timings from a shared development laptop, and re-runs on a busier
  host were slower. With orphans (1.1 million evidence rows, together layout) a
  pass took 209 to 531 ms in the re-run against 162 to 235 ms in the first run,
  and with none (0.72 million rows) 97 to 823 ms against 49 to 55 ms. Whether
  load can push the slowest case (8.5 s) past the 60 s statement timeout was not
  measured. The census runs under that timeout. A timeout fails the pass
  (`relationship_orphan_sweep`) and nothing is swept, so run `VACUUM` after a
  large drain to keep the cheaper index-only scan.

## Operator gate before enabling deletes

1. Leave the flag off. Let a pass run and record the census backlog:
   `eshu_dp_relationship_retention_orphan_generations{table}`. A reading of
   5,000 means "at least 5,000"; take the exact counts with the query below.
2. Estimate the write-ahead log (WAL) the drain will produce: orphan rows
   times the per-row figure below. Reducer replicas do not multiply it. They
   share the backlog through the per-generation advisory try locks (a
   generation one replica holds, the others skip), so more replicas drain it
   sooner at a higher WAL rate, with the same total. The default cap of 100,000
   rows is about 170 MB of WAL per pass at the 1.7 KB per row planning figure,
   and about 1 GB per hour from one replica when capped passes re-run every
   sixth of a 1h poll interval (each replica adds its own passes to that rate).
   The planning figure is for rows scattered across a large table. A local
   PostgreSQL 18 run measured 0.3 KB per row when a generation's rows sit
   together and up to 4.3 KB per row for a pass that starts right after a
   checkpoint, when every first touch of a page writes a full page image.
   The 1.7 KB and 4.3 KB figures predate the row lock record each delete now
   writes (about 55 B more per row, 3%, measured on a smaller corpus) and were not
   repeated at their size. `wal_compression` shrinks the images.
3. If that is too much for the replicas' WAL budget, lower
   `ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ROW_CAP` or lengthen the poll
   interval, then enable the flag.
4. Watch `eshu_dp_relationship_retention_rows_swept_total` and the orphan gauge
   fall, and replica lag stay inside its budget.
5. Read `pg_stat_database.deadlocks` for the database before the drain and after
   it. The delta over the drain window must be 0. In both arrival orders the
   purge succeeds and never sees `40P01`; the sweeper never waits on a row lock,
   skips the held generation as `lock_busy`, and commits the other generations of
   the batch. A non-zero delta means some other statement is in a lock cycle with
   a writer, and the drain is not done until it is found.

Run this read-only count on a replica or inside a read-only transaction. It
reads the same predicates the sweep uses, and the statement timeout keeps a
slow scan from holding a connection:

```sql
BEGIN READ ONLY;
SET LOCAL statement_timeout = '120s';
SELECT 'evidence' AS kind, count(DISTINCT e.generation_id) AS generations, count(*) AS rows
FROM relationship_evidence_facts e
WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = e.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = e.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg
                  WHERE rg.generation_id = e.generation_id AND rg.status = 'active');
SELECT 'candidates' AS kind, count(DISTINCT c.generation_id) AS generations, count(*) AS rows
FROM relationship_candidates c
WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = c.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = c.generation_id)
  AND NOT EXISTS (SELECT 1 FROM relationship_generations rg
                  WHERE rg.generation_id = c.generation_id AND rg.status = 'active');
SELECT 'generations' AS kind, count(*) AS generations
FROM relationship_generations g
WHERE g.status <> 'active'
  AND NOT EXISTS (SELECT 1 FROM scope_generations sg WHERE sg.generation_id = g.generation_id)
  AND NOT EXISTS (SELECT 1 FROM ingestion_scopes s WHERE s.active_generation_id = g.generation_id);
ROLLBACK;
```

## Metrics, phases and failure reason

| Signal | Meaning |
|---|---|
| `eshu_dp_relationship_retention_orphan_generations{table}` | Gauge of orphan generations at the last census, per table. Saturates at 5,000. A failed census leaves it at the last value. |
| `eshu_dp_relationship_retention_rows_swept_total{table}` | Rows deleted per table. Stays 0 while deletes are off. |
| `eshu_dp_relationship_retention_generations_skipped_total{reason}` | Generations left for a later pass: `lock_busy` (another sweeper holds the generation, an activation holds its generation row, or a purge or writer holds some of its evidence or candidate rows; the batch skipped it without waiting), `lock_timeout` (a table lock, such as DDL, held the batch; the count covers a whole batch of up to 50 generations), `ineligible`. |
| `eshu_dp_relationship_retention_capped_total{cap}` | Passes that stopped with orphans left: `rows`, `deadline`, `list`. |
| `eshu_dp_generation_retention_phase_duration_seconds{phase}` | Phases `census_relationship_orphans` (every run) and `sweep_relationship_orphans` (only when batches ran). |
| `eshu_dp_generation_retention_failures_total{reason="relationship_orphan_sweep"}` | A census or batch failed (log `failure_class=generation_retention_relationship_sweep_error`). |

Metric labels carry table names, reasons and caps only. No scope or generation
identifier is ever a label.

**Span.** One `generation_retention.relationship_sweep` span per pass, with a
`generation_retention.relationship_batch` event per committed batch (the
generations it finished, rows per table, duration). The span carries `deletes_enabled`,
`orphan_generations_listed`, `census_saturated`, `generations_swept` (the
generations the committed batches left with no evidence, candidate or generation
row, which includes one whose rows another replica removed after the census),
`batches` and `capped`.

**Logs.** One `generation retention relationship batch committed` line per
committed batch (`batch_generations`, the generations it finished; rows per
table; `batch_rows_deleted`; `batch_continued`, the generations its row limit cut
and the pass runs again; `duration_ms`). A batch that deleted rows but could not
finish a generation still commits those rows, so `batch_generations` can be 0
while `batch_rows_deleted` is not: a large generation reads as a run of batches
that finish none and continue one, then one that finishes it. One `generation retention relationship batch
left for a later pass` line for a batch that deleted nothing and finished
nothing, with the `lock_busy`, `lock_timeout` and `ineligible` counts (a `WARN`
when a lock timeout ended it). One `generation retention relationship sweep completed` line per pass
(`deletes_enabled`, `orphan_generations`, `census_saturated`, `rows_swept`,
`generations_swept`, `skipped`, `batches`, `capped`, `census_seconds`,
`sweep_seconds`). Lines carry counts and times, never identifiers.

## Locks and repository purges

Each batch is one transaction, and it never waits for a row lock. It sets a
transaction-local `lock_timeout` of 500 ms, takes a per-generation advisory try
lock (lock class `10823`, so two sweepers never take the same generation),
re-checks the orphan rules and locks the generation rows with
`FOR UPDATE SKIP LOCKED`, then selects the evidence rows and the candidate rows
of the claimed generations `FOR UPDATE SKIP LOCKED` and deletes exactly those
rows, and deletes the generation row last, only for a generation with no
evidence and no candidate row left. Each of the two selects stops after 10,000
rows it locked (a row it skipped does not count). A read at the end names the
claimed generations that still have a row. If neither select stopped at its
limit, another session holds what is left: they count as `lock_busy`, keep their
generation row, and a later pass retries them while the rest of the batch
commits. If one did, the generation is only larger than the batch, and the same
pass runs it again before it moves on, within the same row cap and deadline.

A repository purge deletes the evidence, candidates and generation rows of the
repository's scopes in the same table order, so the delete order is the same,
but the lock orders cross: the batch holds the generation row first, the purge
reaches it last. A lock timeout cannot break that cycle, because it only ends
the waiter that carries it, and the purge's own deadlock check (1 s by default)
can fire first and make the purge the victim; that happened 700 and 800 ms after
the purge queued in an earlier design that relied on the timeout. The batch is
wait-free instead. In both arrival orders the purge succeeds and never sees a
`40P01`; the batch never waits on a row lock, skips the held generation as
`lock_busy`, and commits the other generations of its batch. The only wait left
is the purge waiting for the batch, which ends when the batch commits or rolls
back, and a batch holds at most about 20,000 row locks, so the purge of a very
large generation waits for one bounded chunk, never for the whole generation. The 500 ms `lock_timeout` stays as a backstop for the one wait the batch
cannot skip, a table lock held by DDL: that batch rolls back and counts
`lock_timeout`.

## Space, bloat and stopping

Deleted rows become dead tuples. Autovacuum reclaims the space for reuse
inside each table, but `VACUUM` returns disk to the operating system only for
empty pages at the end of a file. If the files must shrink after a large
drain, plan `pg_repack` or a maintenance window. Run `VACUUM` after a bulk
drain so the census's index-only scans stay cheap.

To stop a running sweep, unset the flag and roll the reducer. Committed
batches stay deleted, an interrupted batch rolls back whole, and later passes
take the census only.
