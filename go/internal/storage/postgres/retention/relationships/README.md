# storage/postgres/retention/relationships

Package `relationshipretention` removes relationship evidence, candidate and
generation rows of generations that nothing keeps alive (#10823). The reducer
generation retention runner calls it after its prune, outside the prune
transaction, through the `RelationshipSweeper` port in
`reducer/maintenance/retention`.

## The retention key (Rule A)

A generation's rows are removable when all three hold:

1. no `scope_generations` row for the generation id,
2. no `ingestion_scopes.active_generation_id` equal to it,
3. no `relationship_generations` row for it with status `active`.

Anything else is kept: a generation inside the scope retention window (it has
a `scope_generations` row), a refinalize in flight (a scope pointer names it),
a stuck resolution (its generation row is still `active`).

## What it does

| Function | Role |
| --- | --- |
| `TakeCensus` | Lists orphan generations per table (evidence, candidates, generation rows) in one read-only transaction under a local `statement_timeout`; counts distinct generations per table for the gauge. |
| `Sweep` | Runs the census; with `Options.Deletes` it deletes the listed generations in committed batches under a row cap and a deadline. |

`Options.Deletes` is false by default. A pass with deletes off runs the census
only: no batch transaction opens and no `DELETE` is sent.

## Batch shape

One transaction per batch of up to `BatchGenerations` (50) generations. The
batch never waits for a row lock, so it cannot be one side of a lock cycle with
the #10820 repository purge:

1. `set_config('lock_timeout', '500ms', true)`, first, never zero. It bounds the
   one wait the batch cannot skip, a relation-level lock such as DDL.
2. Per-generation `pg_try_advisory_xact_lock(10823, hashtext(id))`; an id
   another sweeper holds is skipped (`lock_busy`).
3. Classification: Rule A re-checked in this snapshot, generation rows locked
   `FOR UPDATE ... SKIP LOCKED` (a row an activation holds is skipped as
   `lock_busy`; one that stopped qualifying is `ineligible`). Evidence-only
   generations with no generation row are claimed without a lock.
4. `DELETE` evidence, then candidates. Each statement picks up to `BatchRows`
   (10,000) rows of the claimed ids that still satisfy all three predicates
   `FOR UPDATE SKIP LOCKED` in a materialized CTE and deletes exactly those rows,
   so a row another session holds stays where it is and does not count against
   the limit. Then the generation row, only for a generation that has no
   evidence and no candidate row left.
5. Settle: one read names the claimed generations that still have a row. If
   neither picking delete stopped at its limit, another session holds the rest:
   they keep their generation row and count as `lock_busy`. If one did, the
   generation is only larger than the batch: the pass runs it again in its own
   transaction (before the next slice of the census list), under the same row cap
   and deadline, and it is not counted skipped.
6. Commit when the batch deleted a row or finished a generation. A batch that did
   neither ends without a commit. A crash before the commit changes nothing.

A 55P03 from any statement (a table lock, the only wait left) rolls the batch
back and counts every generation of that batch as `lock_timeout`; the pass
continues with the next batch. A deadlock (40P01) or any other error ends the
pass and is counted as a failure.

## Limits and what saturates

- `BatchGenerations` = 50 (about 2,650 rows in the measured corpus).
  `BatchRows` = 10,000 per picking delete, so a batch deletes at most 20,050 rows
  however large one generation is: before that bound a single generation of
  125,000 evidence rows ran as one batch of 125,000 rows. The evidence note holds
  the timings and the WAL per row.
- `DefaultRowCap` = 100,000 rows per pass (about 170 MB of WAL at the 1.7 KB per
  row planning figure); the first batch always runs, so one batch can exceed it.
  Replicas split a backlog through the advisory try locks, so more replicas raise
  the WAL rate, not the WAL of a drain.
- `CensusListLimit` = 5,000: the census lists at most this many generations per
  table, so the orphan gauge reads "at least 5,000" when it shows 5,000. The
  runbook sizes a bigger backlog with a read-only count.

## Telemetry

The runner records the gauge, counters, phases, failure reason and log lines
(see `docs/public/reference/telemetry/generation-retention-relationships.md`).
This package owns the pass span `generation_retention.relationship_sweep`, one
`generation_retention.relationship_batch` event per committed batch, and the
per-batch log lines. No identifier is ever a metric label.
