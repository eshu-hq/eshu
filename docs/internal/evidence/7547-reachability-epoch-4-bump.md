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

## Measurement practice

The QA measurement session ran two statements of the naive gate shape on the
replica with a 60 s statement timeout. The standby cancelled both ("conflict
with recovery", `confl_snapshot` = 2 in `pg_stat_database_conflicts`). The
window was 2026-10-04 around 09:55 to 10:20 UTC. API reads may have been refused
through the 2 s replay fence during it; the dead-code routes map a reader-fence
failure to a retryable 503 with Retry-After (#7523), so the visible effect would
be 503s, not 500s. That is not confirmed: no API logs were read. From now on, replica reads use a statement timeout of 10 s or less,
run `EXPLAIN` without `ANALYZE` first for any new query shape, read replay lag
and the API 5xx rate before and after a heavy read, and run in a quiet window.

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
loader line in `SUMMARY.md`; `LIMIT 10` at epoch 3, which returns 0 candidates). That is the existing steady cost of selecting
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
per-repository project time. Estimated range: about 1 to 12 minutes, which
leaves out the full delete and re-insert of about 844,577 rows described under
write-path properties below, so treat it as a lower-bound-style estimate, not a
ceiling. It is an estimate from the pieces above, not an observation. The
ops-qa deploy watch is the measurement.

Memory: the loader reads roots and edges for all 100 candidates of a cycle
before projecting. In the worst case a batch holds up to the corpus total of
496,637 edges in memory at once; that bound is stated, not measured, and the
reducer RSS trigger below covers it.

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
- The write is idempotent but it is a full rewrite: `replaceCodeReachabilityRepositoryRows`
  deletes every `code_reachability_rows` and `code_root_verdicts` row of the
  (scope, generation, repository) partition and re-inserts the whole snapshot
  in the same transaction (`ON CONFLICT ... DO UPDATE` upserts on the insert).
  Write volume: the drain therefore deletes and re-inserts the reachability
  rows of every active repository, about 844,577 rows in a 2,245 MB table
  including indexes, plus the verdict rows. Expect dead tuples and write-ahead
  log of the order of the table size; the WAL volume and the replica replay lag
  it causes are unmeasured.
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
- Replica replay lag against the API's 2 s fence. The fence applies to every
  PostgreSQL-backed API and MCP business read, not only the dead-code routes:
  business reads receive only the reader pool, every borrow is fenced by a 2 s
  replay timeout, and there is no writer fallback, so lag over 2 s answers 503
  `backend_unavailable` with Retry-After on every such route. Watch the
  API-wide 503 `backend_unavailable` rate and the existing
  `eshu_dp_postgres_reader_stage_duration_seconds` panel filtered to
  `stage="reader_replay"`, where `outcome="deadline"` marks a replica that missed
  the fence.
  Lag can exceed WAL-volume expectations because the reader is asynchronous
  (`numSynchronousReplicas: 0`) and `max_standby_streaming_delay` is not set in
  the chart values (PostgreSQL default 30 s; the live value was not checked),
  so any long replica read during the drain can hold replay for up to 30 s.
  During the drain the only replica read should be the census, with a 10 s
  timeout.
- Dead tuples and autovacuum on `code_reachability_rows` (the drain rewrites
  every row of it).
- Reducer RSS against `GOMEMLIMIT`.
- Reducer restarts and exit errors. A projection error (`load code reachability
  inputs`, `delete code reachability rows`, `upsert code reachability
  watermark`) is returned by `CodeReachabilityProjectionRunner.Run` and
  recorded by the service, which cancels the whole reducer service
  (`internal/reducer/service.go` `recordErr`). One repository that fails every
  time is selected again after the restart (oldest first), so a failing
  repository can crash-loop the reducer and stall all reducer work. That
  behavior predates this change; the drain makes it more likely, because 795
  re-projections run in about 8 back-to-back cycles. A restart during the
  drain is a stop signal.
- Census caveat: the census query joins only `ingestion_scopes`; the loader
  also requires an acceptance row, an active generation and a completed
  intent. A residual above 0 after the completion log lines stop means
  non-candidates, not a stuck drain.

Deploy path and stop lever. ops-qa deploys by an owner pin commit in the
GitOps repository. Automated sync is currently paused on the ops-qa
Application (`autoSync: false` in its config, paused for the NornicDB to Neo4j
cutover), so merging the pin changes Git only: the owner runs a manual sync of
the Application to deploy, and the same holds for a rollback. `selfHeal` is
configured but is a sub-option of automated sync, so it does nothing while
automated sync is off. Merging the epoch bump to `main` does not deploy it.
ops-prod is a separate overlay pinned separately.

Stop levers. The fast lever is scaling the reducer deployment to 0 by hand
(`kubectl -n eshu scale deployment/eshu-resolution-engine --replicas=0`; confirm the name with `kubectl -n eshu get deploy`). It stays at 0 while automated sync is off, the
Application shows OutOfSync, and the owner restores the replicas afterwards;
in-flight per-repository transactions commit or roll back whole. The durable
lever is re-pinning the previous image: the epoch-3 binary selects nothing new
because every comparison is `<`, but it needs a merged pin change and a manual
sync, so it is slower than the fast lever. Reverting the constant on `main` is
the slowest path. If automated sync is re-enabled before the drain (it is
meant to be restored after the cutover health proof), scaling to 0 is reverted
by self-heal and re-pinning becomes the only lever; check the Application's
sync policy before relying on either.

Stop triggers, with a time box. Stop the drain (the fast lever above, with
re-pinning the previous image as the durable fallback) if reader-fence 503s
(replay lag over the 2 s fence) persist for 2 consecutive minutes, if the
reducer restarts or exits with a projection error, if reducer RSS exceeds 50%
of `GOMEMLIMIT`, or if the census has not reached 0 within 60 minutes of the
pod start. The owner may tighten these. The ops-prod pin waits for a completed
ops-qa drain (census 0) with a flat API-wide 503 rate.

Concurrency of the burst: up to 8 concurrent per-repository rewrite
transactions on ops-qa (`ESHU_REDUCER_WORKERS` is 8 in the ops-qa values and is
clamped to the CPU count; when it is unset the runner uses min(CPUs, 4) on Neo4j). The
runner takes no lease or claim, so two reducer replicas would select the same
100 candidates and both run the full delete and re-insert on the same
repositories at the same time: duplicate work and concurrent rewrites, not
only a double stamp.

Interaction with the loader restructure (a separate performance PR, slice D).
Merge order between the two is free. It shortens each cycle's candidate
selection from about 4.7 s to about 0.7 s, which removes the natural pause
between 100-repository write bursts. Total write volume is the same either
way, and pacing is not a mechanism this change relies on. Pin ops-qa once with
both, so the no-pause case is the one measured, and say which was measured. The
loader gate also narrows candidates (delta generations and incomplete
materialization), so with it the census residual can include gated-out runs
that stay at epoch 3 until their next full generation; the cross-repo reader
reports those as `unknown_needs_evidence`. On the measured QA snapshot the
candidate-set hash was identical with and without the gate, so no repository is
excluded today. The 1 to 12 minute estimate above must be re-derived with about
0.7 s per cycle if the gate is in the pin; either way it is dominated by the
unmeasured write side.

Pacing is not a lever: do not lower the batch limit, the worker count, or the
poll rate to slow the drain.

## Rollback

Revert the constant to 3 and redeploy. No data repair is needed. Epoch-4
stamps stay valid because every comparison is `<` (the loader and the #7573
predicate), and nothing tests equality. A half-drained mix of epoch 3 and 4
is consistent. Re-applying the bump resumes the drain. During a rolling deploy
an old binary can re-stamp a repository at epoch 3 on a new ingest; the new
binary re-selects it, so this is bounded.

## Owed items, now closed

- The remote rerun of `BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff`
  from #7570 ran on 2026-10-04 on the quiet remote validation host (16 CPUs,
  load about 0.01, Go 1.26.2), from git worktrees of the reviewed commits:
  base `791078e81` against the #7570 merge `4529a8f9b`, interleaved pairs,
  `-cpu=1`, 10 samples per side, benchstat. The benchmark itself is new in
  #7570, so its file was copied from the head commit into the base worktree
  to run the same benchmark on both sides. Result: the frontier-at-cutoff
  benchmark took 19.65 ms (±3%) on base and 20.36 ms (±4%) on head, +3.6%
  (p<0.001); the older 12-deep benchmark, which never reaches the cutoff, took
  102.6 ms (±2%) and 105.0 ms (±2%), +2.3% (p=0.035). Bytes and allocations per
  call are identical on both. So #7570's change to the walk (the frontier scan plus the stamping
  code around it) has a small, statistically detectable cost of about 0.7 ms
  on a 50,000-node graph; the older benchmark
  moving about 2% as well suggests code layout or the extra branch, not the new
  scan alone. It is well under the repo's 10% stop-and-profile bar and small against
  the ops-qa drain cycles (about 5 s on average, 15.7 s slowest). The
  earlier laptop result ("no significant difference") was taken on a noisy
  machine and is superseded by this one.
- The end-to-end drain on ops-qa ran on 2026-10-04 after the owner pinned and
  synced the build: 795 active watermarks at epoch 4 and none below, 8 cycles
  in about 42 seconds (slowest cycle 15.7 s), 77,825 rows written, 384
  truncation stamps (379 `no_roots`, 5 `max_depth`, no `max_visited`), no
  reducer restart (261 MiB), no reader-fence deadline or 503. The loader gate
  held nothing back. The figures are on eshu-hq/eshu#7547.

NOT_CHECKED for the drain: dead tuples and autovacuum on the rewritten tables
(the replica's statistics views were empty), and the loader's `LIMIT 100`
and generic-plan behavior on ops-qa.
