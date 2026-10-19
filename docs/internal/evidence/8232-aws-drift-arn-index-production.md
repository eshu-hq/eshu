# Evidence: migration 180 index build, plan, and drift run on the QA cluster (#8232)

Operator observation closing the production gap #7666 left as NOT_CHECKED.
Baseline: `docs/internal/evidence/7666-aws-drift-arn-index.md` (14-20 minutes
per `aws_cloud_runtime_drift` run, writer held at 12 cores, 11,164,992 buffers
on the 1/20-scale shim over 1.68M rows).

## Index state

`fact_records_terraform_state_resource_arn_idx` is valid, ready, and live on
the QA primary (`pg_index` all true), 32 kilobytes over a 13,745,504-row,
21 GB `fact_records` (ledger:8232-opsqa-arn-index-state). Only 403
`terraform_state_resource` rows exist there, 134 carrying an ARN. Migration 180
recorded `applied_at` 2026-10-17 20:42:18 UTC; the next migration in the same
applier run recorded 8.37 s later (ledger:8232-opsqa-migration-gap),
which bounds a sibling concurrent index build on the same table to
seconds, not minutes.

Build wall time and WAL volume for migration 180 itself are NOT_CHECKED: the
Oct-17 applier pod and its per-migration `duration_ms` logs are gone; the QA cluster's
PostgreSQL 18.3 runs only `pgaudit` in `shared_preload_libraries` (no
`pg_stat_statements`), and rebuilding the index to measure would be a cluster
DDL mutation, which needs an arbiter review this observation does not have.

## Plan with real statistics

Exact shipped `listActiveStateResourcesForAWSARNsQuery` text (extracted from
the constant, fences intact), `PREPARE (text, text, jsonb)`, elbv2 scope with
77 ARNs, second of two back-to-back `EXPLAIN (ANALYZE, BUFFERS)` runs.
`fact_records` statistics are real and fresh (`last_autoanalyze` 2026-10-19
13:39 UTC).

- Custom plan: Execution Time 2.727 ms (ledger:8232-opsqa-drift-state-join-custom).
- Generic plan (`force_generic_plan`, the shape pgx takes after five
  executions): Execution Time 2.787 ms
  (ledger:8232-opsqa-drift-state-join-generic).
- Root shared buffers 235 on the 13.7M-row table
  (ledger:8232-opsqa-drift-state-join-buffers). For scale context, the shim
  baselines were 11,164,992 buffers without the fix and a 1,625-2,668 band at
  379 ARNs with it; the production cell is 77 ARNs on a larger table, so the
  load-bearing comparison is plan shape, below.

Per-ARN probe, not a walk: the ARN index node runs 77 loops with 77 index
searches returning 6 rows in 160 buffers, and the `ingestion_scopes` join sits
outside the fence at 6 loops. No `fact_records` node loops more than once per
requested ARN. Full plans live in throwaway probe output on the operator host
and are not committed.

The slowest scope in the wave (sns, 33.54 s handler for 1 resource) got its
own probe: 4 ARNs, 4 index searches, 0.262 ms, 38 buffers
(ledger:8232-opsqa-drift-sns-join-statement). The join contributes under a
millisecond there, so the 33 s is loader, classifier, and canonical-write
work, not the #7666 statement.

## Drift run wall time

The running reducer (`ghcr.io/eshu-hq/eshu:sha-3d7cb9c`, deployed 2026-10-19
06:37 UTC) contains the B3 fix (`git merge-base --is-ancestor` of the fix
commit returns 0). The 2026-10-19 wave after the index went valid ran 19 AWS
scopes from 13:53:03Z to 13:54:34Z: 91 s wall
(ledger:8232-opsqa-drift-wave-wall). Slowest scope 33.54 s
(ledger:8232-opsqa-drift-scope-max); largest scope elbv2, 77 resources plus 77
canonical writes, 11.062 s (ledger:8232-opsqa-drift-scope-elbv2). The run-level
corpus differs from the #7666 baseline (13.7M rows here against ~36.9M there),
so the wall-time comparison is directional; the statement-level probe above is
the scale-independent proof.

## Writer saturation

No. The mechanism that pinned the writer (the 14-20 minute statement) now
executes in about 3 ms, every observed scope run completes in seconds, and a
7 h scan of the reducer log shows zero deadlock, lock-timeout, pool-exhaustion,
or statement-timeout lines. No run exceeds the contribution budget in the
baseline note, so no follow-up performance issue is filed.

## Not checked

- Exact migration 180 `CREATE INDEX CONCURRENTLY` wall time and WAL bytes
  (see Index state).
- `eshu_dp_reducer_run_duration_seconds` history for Oct 17-18: the monitoring
  namespace denies this role exec and port-forward, so only pod-log windows
  were observable.
