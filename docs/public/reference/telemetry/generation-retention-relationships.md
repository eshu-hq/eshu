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

- The runner takes the census and, when enabled, sweeps at most once per poll
  interval (`ESHU_GENERATION_RETENTION_POLL_INTERVAL`, `1h` by default), after
  the search index sweeps. A failed pass is recorded and logged and does not
  fail the retention pass.
- A pass deletes at most the row cap, and stops after a quarter of the poll
  interval. Batches hold at most 50 generations. The first batch always runs,
  so one pass can exceed the cap by up to one batch.
- After a pass that stopped on the row cap, the deadline or the census list
  limit, the runner re-runs after a sixth of the poll interval so a backlog
  drains in hours. It never re-runs early after a failed pass, a clean pass, or
  a pass that skipped a batch on `lock_timeout`.
- The census lists at most 5,000 orphan generations per table; the rest are
  listed by later passes. The sweep re-lists from the lowest id each pass.
- The census reads every generation id of each relationship table once: 0.2 to
  0.3 s warm (0.5 to 1.2 s on a first pass) on 3.6 million evidence rows with no
  orphans, 2.4 to 8.5 s with 2 million orphan rows among 5.6 million, the slow
  end when the planner reads the evidence table with a parallel sequential scan.
  Those are timings from a shared development laptop; a re-run on a busier host
  took a no-orphan pass up to 17 times as long. The census runs under a 60 s
  statement timeout. A timeout fails the pass (`relationship_orphan_sweep`) and
  nothing is swept, so run `VACUUM` after a large drain to keep the cheaper
  index-only scan.

## Operator gate before enabling deletes

1. Leave the flag off. Let a pass run and record the census backlog:
   `eshu_dp_relationship_retention_orphan_generations{table}`. A reading of
   5,000 means "at least 5,000"; take the exact counts with the query below.
2. Estimate the write-ahead log (WAL) the drain will produce: orphan rows
   times 1.7 KB times reducer replicas. The default cap of 100,000 rows is
   about 170 MB of WAL per pass, and about 1 GB per hour per reducer replica
   when capped passes re-run every sixth of a 1h poll interval. That is a
   planning figure for rows scattered across a large table. A local PostgreSQL
   18 run measured 0.3 KB per row when a generation's rows sit together and
   up to 4.3 KB per row for a pass that starts right after a checkpoint, when
   every first touch of a page writes a full page image. `wal_compression`
   shrinks the images.
3. If that is too much for the replicas' WAL budget, lower
   `ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ROW_CAP` or lengthen the poll
   interval, then enable the flag.
4. Watch `eshu_dp_relationship_retention_rows_swept_total` and the orphan gauge
   fall, and replica lag stay inside its budget.

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
| `eshu_dp_relationship_retention_generations_skipped_total{reason}` | Generations left for a later pass: `lock_busy`, `lock_timeout`, `ineligible`. A `lock_timeout` count covers a whole batch of up to 50 generations. |
| `eshu_dp_relationship_retention_capped_total{cap}` | Passes that stopped with orphans left: `rows`, `deadline`, `list`. |
| `eshu_dp_generation_retention_phase_duration_seconds{phase}` | Phases `census_relationship_orphans` (every run) and `sweep_relationship_orphans` (only when batches ran). |
| `eshu_dp_generation_retention_failures_total{reason="relationship_orphan_sweep"}` | A census or batch failed (log `failure_class=generation_retention_relationship_sweep_error`). |

Metric labels carry table names, reasons and caps only. No scope or generation
identifier is ever a label.

**Span.** One `generation_retention.relationship_sweep` span per pass, with a
`generation_retention.relationship_batch` event per committed batch (batch
generations, rows per table, duration). The span carries `deletes_enabled`,
`orphan_generations_listed`, `census_saturated`, `generations_swept`,
`batches` and `capped`.

**Logs.** One `generation retention relationship batch committed` line per
committed batch (`batch_generations`, rows per table, `batch_rows_deleted`,
`duration_ms`). One `generation retention relationship batch left for a later
pass` line for a batch that swept nothing, with the `lock_busy`,
`lock_timeout` and `ineligible` counts (a `WARN` when a lock timeout ended
it). One `generation retention relationship sweep completed` line per pass
(`deletes_enabled`, `orphan_generations`, `census_saturated`, `rows_swept`,
`generations_swept`, `skipped`, `batches`, `capped`, `census_seconds`,
`sweep_seconds`). Lines carry counts and times, never identifiers.

## Locks and repository purges

Each batch is one transaction. It sets a transaction-local `lock_timeout` of
500 ms, takes a per-generation advisory try lock (lock class `10823`, so two
sweepers never take the same generation), re-checks the orphan rules and
locks the generation rows with `FOR UPDATE SKIP LOCKED`, then deletes evidence,
candidates and the generation row last. A repository purge also deletes the
evidence, candidates and generation rows of the repository's scopes, in that
order. When a purge holds rows a batch needs, the batch waits at most 500 ms,
rolls back and counts as `lock_timeout`. That is below the 1 s default
`deadlock_timeout`, so with the default the batch always gives way first: the
purge is never the deadlock victim and neither side sees a deadlock. A later
pass picks the generations up again.

## Space, bloat and stopping

Deleted rows become dead tuples. Autovacuum reclaims the space for reuse
inside each table, but `VACUUM` returns disk to the operating system only for
empty pages at the end of a file. If the files must shrink after a large
drain, plan `pg_repack` or a maintenance window. Run `VACUUM` after a bulk
drain so the census's index-only scans stay cheap.

To stop a running sweep, unset the flag and roll the reducer. Committed
batches stay deleted, an interrupted batch rolls back whole, and later passes
take the census only.
