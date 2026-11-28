# #11615 requeue-reason hot-path proof

## Summary

Labeling every reducer requeue with `last_requeue_reason` changes no query
plan shape and adds only the reason's tuple width: single-row reopen and
retry keep their index plans and buffers, the two CASE-scheduler rewrites
add one primary-key probe behind a `FOR NO KEY UPDATE` lock they already
took, the claim adds a pass-through column, and the per-reason observer
GROUP BY costs +16 ms at 200k rows on the same 60 s poll. WAL grows ~13%
(~110-150 B) per requeue write from the wider tuple; no new index, statement,
or round trip anywhere in the change.

## Hot-path measurements

Performance Evidence: disposable PostgreSQL 18.6 (aarch64, localhost), full 388-migration
bootstrap, 20k-row corpus plus probe rows, `VACUUM ANALYZE`, identical
fixture state per side (reset between sides), `EXPLAIN (ANALYZE, BUFFERS)`
rolled back, WAL from `pg_current_wal_insert_lsn` deltas around one
autocommit execution per side:

- Reopen single row: plan identical (Index Scan on the work-item pkey in
  both), buffers 26 vs 26, WAL 968 B vs 1080 B (+112 B, the label).
- Retry claimed row: plan identical (claim_until index in both), buffers
  24 vs 25, WAL 1112 B vs 1264 B (+152 B; the reason rides a bind param).
- Schedule (succeeded row) and retouch (failed row): old single-UPDATE vs
  new target-CTE + `RETURNING` transition boolean. Same pkey probe plus one
  extra pkey probe for the pre-image (buffers 29 vs 30 / 28 vs 29, +1 index
  search each); 0.08 ms vs 0.34 ms per schedule on a cold fixture. The CTE
  takes the same `FOR NO KEY UPDATE` row lock the UPDATE took, so lock
  class and ordering are unchanged; the pre-image is what makes the
  transitioned-row count exact, which the counter contract requires.
- Claim with the added column: index-driven plan throughout
  (`cross_scope_source_v2_idx`, pkey, live-lease uniq, `claim_until_idx`),
  0.279 ms at 20k rows, no seq scan on the hot path. The column is a
  pass-through in `RETURNING` and the outer `SELECT`: no predicate, join,
  `ORDER BY`, or rank references it.
- Observer GROUP BY at 200k rows (same fixture and storage state):
  depth 80.1 ms vs 96.5 ms (+16 ms, the wider sort key; bound: under
  150 ms at 200k on this profile, under 0.3% duty on a 60 s poll),
  oldest age 75.2 ms vs 74.4 ms (noise). No temp-table or work_mem change
  vs the base poll.

Benchmark Evidence: `BenchmarkReducerQueueClaimDeepQueue` (depth 100k: 171 ms/op; depth 1M:
1243 ms/op; 132 allocs/op, unchanged shape) and
`BenchmarkReducerQueueClaimReadinessGateGrowth` pass on the rebuilt binary
with the column present. The claim bench's minimal schema needed migration
361 alongside 088, following that file's own precedent comment.

No-Regression Evidence: `go test ./internal/storage/postgres/...` plus telemetry, queue, poison,
backfill/targeted, contract, producer/poison runners, admin store, and
bootstrap-index all pass; the requeue guard suite
(`TestEveryRequeueWriterSetsReason`, planted RED/GREEN pair, vocabulary
closure, migration shape pins) is green; `golangci-lint` on every touched
package reports 0 issues. Two soak tests
(`WinnersQuietAbsoluteReproveLive`, `WinnersStagingAutovacuumSoakLive`)
refuse on this host (load 15 on a shared machine; they exercise no touched
path) and are NOT_CHECKED here.

Observability Evidence: `eshu_dp_reducer_requeue_total{reason}` counts transitioned rows only
(rows-matched never counts; trigger conversions never double-count;
rolled-back transactions never count, with the single documented exception of
in-tx waiting wakes: in-tx waiting wakes via `wakeWaitingByKey`
(`go/internal/storage/postgres/reducer_waiting_store.go:351`) share the
pre-existing sibling-counter pattern, so a rollback after the wake overcounts
both equally while autocommit paths stay exact; #11624 removes this exception
by hoisting the emission out of caller-owned transactions),
`eshu_dp_reducer_requeue_depth` and
`eshu_dp_reducer_requeue_oldest_age_seconds` ride the existing observer
poll, and the stuck-queue runbook gains the per-reason breakdown. The
`legacy_unclassified` depth series is the rollout ramp and must trend to
~0 for reducer rows.
