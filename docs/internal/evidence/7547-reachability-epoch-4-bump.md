# #7547 slice E: `CodeReachabilityVerdictSchemaEpoch` 3 to 4

This change bumps `CodeReachabilityVerdictSchemaEpoch` from 3 to 4 in
`go/internal/storage/postgres/code/reachability/store.go`. PR #7570 changed
which snapshots the reducer stamps `truncated = true` (zero roots, and a depth
cutoff with unseen targets). A watermark written before that change holds the
old `truncated = false`. The bump makes the pending-input loader re-select
every active repository once, so each one is re-projected and re-stamped.
Nothing else changes: no verdict, row, schema, SQL, or queue semantics.

The arbiter ruling for #7547 allowed this bump on its own PR once the PR
carries the measured QA numbers, a write-side proxy, a drain estimate, a watch
plan, and a rollback statement. The end-to-end drain time is measured by the
ops-qa deploy watch below. That watch must finish before any production pin.
It is not a pre-merge gate.

## Where the numbers come from

Every QA figure here is verified on one data copy: the ops-qa read replica
(PostgreSQL 18.3, read-only session, statement timeout set), read by the #7547
measurement agent on 2026-10-04. Live data moved between reads (795 and 796
watermark rows appear), so treat the counts as one snapshot. Nothing here is a
repeat run on a second copy. The raw outputs (`SUMMARY.md`, `t1.out`,
`t1c.out`, `j3a.out`, `j3b.out`, `t2_cur.out`, `id.out`) sit in the
measurement agent's local `7547-gate` scratch directory and are not committed.

## What the bump selects

Performance Evidence: on the QA replica, 795 active repository watermarks are
all at epoch 3 with `truncated = false` (`t1.out`, `j3a.out`). At epoch 4 the
loader's candidate set is all 795 of them: the set hash is identical between
the current loader SQL and a restructured candidate query (`t1c.out`), and the
epoch-3 set is empty in both. So the bump re-selects exactly the stamped
repositories, no more. Of the 795, 379 have zero roots (`j3a.out`). They stay
`truncated = true` after the drain, with an INFO `no_roots` log line each.

The 795 acceptance rows carry 496,637 active `code_calls` and
`inheritance_edges` intent rows (`j3a.out`). Per repository: p50 8, p95 2,913,
max 61,633. The stored payload size is 335 MB by `pg_column_size`. That is the
stored (possibly compressed) size, so it is a lower bound on the bytes the
loader reads. `code_reachability_rows` holds 844,577 rows (`id.out`, exact
count) in a 2,245 MB table including indexes (`pg_total_relation_size`; the
planner estimate in the same file is 840,819 rows). The watermark table has
8,689 rows across all generations, all at epoch 3; only the 795 on the active
generation are loader candidates.

## Cost evidence

Performance Evidence: the loader's candidate selection takes 8,385 ms cold and
4,698 ms and 4,667 ms warm per call on the QA replica (`t2_cur.out` and the
loader line in `SUMMARY.md`). That is the existing steady cost of selecting
candidates, paid on every shared-projection cycle today. The bump does not add
to it and does not change the query. The largest repository (61,633 edge rows)
loads its edges in 360 to 413 ms and its roots in 296 to 888 ms (`j3b.out`).
The BFS and the DELETE and INSERT of its reachability rows come on top and are
not measured here.

Write-side proxy, from the ops-qa reducer logs: five
`code reachability projection completed` lines from the resolution-engine pod,
read-only `kubectl logs`, 2026-10-04 10:24 to 10:25 UTC. They show
`input_count` 1 to 7, `row_count` 3 to 17, `snapshots_truncated` 0, and
`duration_seconds` 4.95 to 6.10 per cycle. This is a proxy only. Each cycle's
duration is dominated by the roughly 4.7 s candidate selection above, so the
lines show that small-repository projection is cheap and say nothing about the
largest repositories.

No-Regression Evidence: the pin test
`TestCodeReachabilityVerdictSchemaEpochBumpedForTruncationSemantics` and the
live test `TestCodeReachabilityTruncationEpochBumpRestampsWatermarks` prove the
behavior: an epoch-3 watermark is re-selected at epoch 4, re-stamped at epoch
4, and not re-selected again. Both were seen failing with the constant at 3
(throwaway postgres:18-alpine) before the bump to 4.
`TestCodeReachabilityPendingInputsPlanAtEpochBump` asserts planner node-class
equality at one intent per repository. It is a plan-shape guard, not
performance proof, and this document does not cite it as one.

## Drain estimate (not measured end to end)

The shared projection runner takes `ESHU_SHARED_PROJECTION_BATCH_LIMIT`
(default 100) repositories per cycle. 795 repositories at batch 100 is at
least 8 cycles. Each cycle pays about 4.7 s of candidate selection plus the
per-repository project time. Estimated upper bound: about 1 to 12 minutes.
This is an estimate from the pieces above, not an observation. The ops-qa
deploy watch is the measurement.

The runner polls every `ESHU_SHARED_PROJECTION_POLL_INTERVAL` (default
500 ms). While a cycle processes intents, `Run` re-polls without waiting, so
the interval does not pace the drain. Candidates are ordered by completed time,
oldest first, so a freshly ingested repository queues behind the stale ones.
Reachability freshness for new ingests therefore lags for the length of the
drain.

Write-path properties, from the code:

- Each repository is replaced in one transaction
  (`CodeReachabilityStore.ReplaceRepositoryRows`): rows, verdicts, and the
  watermark commit together or not at all.
- The write is idempotent: `ON CONFLICT ... DO UPDATE` upserts plus a
  primary-key-scoped DELETE of rows the new snapshot no longer holds.
- The drain is self-extinguishing: once a repository is stamped at epoch 4 the
  loader's `verdict_schema_epoch` comparison no longer selects it.
- A partial drain is resumable: repositories already stamped stay stamped, and
  the rest are selected on the next cycle.

## Consequences

- About 379 zero-root repositories stay `truncated = true` and log INFO
  `no_roots`. A handful of `max_depth` WARN lines are expected; `max_visited`
  is expected to be 0.
- Dead-code single-repository reads are unaffected: nothing on `main` reads
  the `truncated` bit.
- The cross-repo reader (#7573, draft) must merge only after this drain has
  completed on the target. Its predicate treats a watermark below the compiled
  epoch as a coverage gap, so deploying it during the drain would read every
  cross-repo answer as `unknown_needs_evidence` until the drain ends.

## Watch plan (existing signals only)

Observability Evidence: no new metric, span, log key, status field, or worker.
The deploy watch uses signals that already exist:

- The completion log per cycle: `input_count`, `row_count`,
  `snapshots_truncated`, `duration_seconds`. A cycle with no input logs
  nothing, so drain completion is proven by a replica census, run with a 10 s
  statement timeout:

  ```sql
  SELECT count(*)
  FROM code_reachability_repository_watermarks w
  JOIN ingestion_scopes s
    ON s.scope_id = w.scope_id AND s.active_generation_id = w.generation_id
  WHERE w.verdict_schema_epoch < 4;
  ```

  The drain is complete when this returns 0.
- `truncation_reason` lines: expect about 379 `no_roots` at INFO, a handful of
  `max_depth` at WARN, and 0 `max_visited`. Many more WARNs means stop.
- Replica replay lag against the API's 2 s fence, and the 5xx rate on the
  dead-code routes.
- Dead tuples and autovacuum on `code_reachability_rows`.
- Reducer RSS against `GOMEMLIMIT`.

A staged rollout (ops-qa drained fully with a flat API error rate before any
production pin) is required if any of these holds:

- the log proxy predicts a drain over 30 minutes, or any cycle exceeds 60 s at
  batch 100;
- reducer RSS exceeds 50% of `GOMEMLIMIT`;
- replica lag trips the 2 s fence, or the dead-code 5xx rate rises;
- the target runs more than one reducer replica (a rolling deploy can
  double-stamp).

Pacing is not a lever: do not lower the batch limit, the worker count, or the
poll rate to slow the drain.

## Rollback

Revert the constant to 3 and redeploy. No data repair is needed. Epoch-4
stamps stay valid because every comparison is `<` (the loader and the #7573
predicate), and nothing tests equality. A half-drained mix of epoch 3 and 4
is consistent. Re-applying the bump resumes the drain. During a rolling deploy
an old binary can re-stamp a repository at epoch 3 on a new ingest; the new
binary re-selects it, so this is bounded.

## Still owed

- The remote rerun of `BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff`
  from #7570 (base `791078e81` against the #7570 head, `-cpu=1 -count=10`,
  benchstat). It may be folded into the deploy watch.
- The end-to-end drain measurement on ops-qa, before any production pin.
