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

One transaction per batch of up to `BatchGenerations` (50) generations:

1. `set_config('lock_timeout', '500ms', true)`, first, never zero.
2. Per-generation `pg_try_advisory_xact_lock(10823, hashtext(id))`; an id
   another sweeper holds is skipped (`lock_busy`).
3. Classification: Rule A re-checked in this snapshot, generation rows locked
   `FOR UPDATE ... SKIP LOCKED` (a row an activation holds is skipped as
   `lock_busy`; one that stopped qualifying is `ineligible`). Evidence-only
   generations with no generation row are claimed without a lock.
4. `DELETE` evidence, then candidates, then the generation row, each taking
   only the claimed ids and re-asserting all three predicates in its own
   `WHERE`.
5. Commit. A crash before the commit changes nothing.

A 55P03 from any statement rolls the batch back and counts every generation of
that batch as `lock_timeout`; the pass continues with the next batch. A
deadlock (40P01) or any other error ends the pass and is counted as a failure.

## Limits and what saturates

- `BatchGenerations` = 50 (about 1,700 rows and 1.4 MB of WAL measured): a batch
  is bounded in generations, not rows.
- `DefaultRowCap` = 100,000 rows per pass (about 170 MB of WAL); the first batch
  always runs, so one batch can exceed it.
- `CensusListLimit` = 5,000: the census lists at most this many generations per
  table, so the orphan gauge reads "at least 5,000" when it shows 5,000. The
  runbook sizes a bigger backlog with a read-only count.

## Telemetry

The runner records the gauge, counters, phases, failure reason and log lines
(see `docs/public/reference/telemetry/generation-retention-relationships.md`).
This package owns the pass span `generation_retention.relationship_sweep`, one
`generation_retention.relationship_batch` event per committed batch, and the
per-batch log lines. No identifier is ever a metric label.
