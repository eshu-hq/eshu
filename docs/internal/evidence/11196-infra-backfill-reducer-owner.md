# Evidence: the reducer runs the infra read model backfill (#11196)

Records the performance, concurrency and observability evidence for starting
`inventory.RunBackground` from the reducer so a fresh install, a wiped
database or a marker bump reaches the infra read model on its own. The design
and the lock analysis are in
`docs/internal/design/11196-infra-backfill-reducer-owner.md`.

## Change shape

A new leaf runner, `infra.BackfillRunner`, wraps the existing background
backfill and starts beside the other reducer side runners. The backfill itself
is unchanged: one advisory transaction lock per repository, taken first, with
the marker insert outside the lock. No worker count, queue, lease, batch size
or claim path changes. Dead code that nothing called (`StartInfraInventoryBackfill`,
`RunStartupBackfills`) is removed.

## Performance Evidence:

Backend: PostgreSQL 18.6 on a private container, one host at load1 near 29
with iowait 34 to 44 percent, so only ratios are comparable. Input shape: 200
repositories with 1,000 content rows each, 150,000 infra table rows (the shape
of the default gate corpus). Terminal state: one marker row, the table equal to
the content in both directions.

| Replicas | Wall time | Derive transactions | WAL bytes |
| --- | --- | --- | --- |
| 1 | 16.259 s | 200 | 123,127,792 |
| 3 | 32.778 s (2.0x) | 600 (3.0x) | 291,725,808 (2.4x) |

Every replica races on the per-repository lock, so a replica set repeats work
that another replica already did. The cost is one time per install or marker
bump and bounded by the replica count; it does not affect correctness. A skip
for an already-derived repository needs a per-repository marker written inside
the derive transaction, which is a schema change and is recorded as a follow-up
in the design record, not added here.

## No-Regression Evidence:

- Four replicas plus a live content writer, six repositories of 2,000 rows each,
  under `-race`: 0 errors, one marker row, table equal to content, and each
  replica derived each repository once (24 derive transactions). Three of three
  author runs and nine of nine independent review runs passed.
- Seeded violation: with the advisory lock removed from the derive path, the
  independent review saw nine of ten runs fail (five with a resurrected stale
  row, four with a `40P01` deadlock). The guard is probabilistic, not
  deterministic, and runs on the scheduled live runner.
- A failing-first wiring test shows a product runtime starts the backfill: three
  leaf tests, the reducer build test and the service start test all failed
  before the runner was wired and pass after.
- The runner returns nil when its context is cancelled, so a shutdown does not
  cancel the other reducer components.

## Observability Evidence:

No-Observability-Change: the runner emits the existing
`eshu_dp_infra_inventory_backfill_runs_total` counter and the
`infra_inventory.backfill` log events from `inventory.RunBackground`; the
reducer passes its instruments and logger through. No new metric, span or log
key is added. An operator now sees those events from the reducer process on a
fresh install, where before nothing started the backfill and the status report
showed `backfilling` forever.
