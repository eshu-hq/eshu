# #7319: fence the git delta baseline against activation

## Problem

The git collector diffs a delta generation from the scope's active commit,
which it reads once at the start of its cycle without a lock
(`lastProjectedCommitSHAQuery`). Another generation can activate while the delta
is parsed, committed, or projected. Ack never compared the commit the delta was
diffed from with the commit that was active when it activated, and the delta
generation did not record that commit, so the overlap could not be seen after
the fact either.

Root-Cause Evidence: on `c8b5145212` (origin/main), the two-session live test
`TestProjectorAckFencesDeltaAgainstActivationWhileWaiting`
(`go/internal/storage/postgres/projector_queue_delta_baseline_race_live_test.go`,
written to compile on main) failed against PostgreSQL 18.6. Session 1 held
G_B's Ack open; session 2, the production Ack of G_D (diffed from A), waited on
the scope row and resumed after G_B activated:

```text
race outcome: ack_err=<nil> gen-d=active gen-b=superseded pointer=gen-d work=succeeded class=
Ack(gen-d) after gen-b activated = <nil>, want ErrWorkSuperseded
--- FAIL: TestProjectorAckFencesDeltaAgainstActivationWhileWaiting
```

## What changed

- Migration 148 adds nullable `scope_generations.delta_baseline_commit_sha`
  (one statement, no default, index, backfill, or CHECK).
- The collector records the baseline on the delta:
  `updateRepository` sets `GitSyncDelta.BaselineCommitSHA`; it travels through
  `SelectedRepository`, `RepositorySnapshot`, and `ScopeGeneration` to
  `upsertScopeGenerationQuery`. The native and webhook selectors share
  `buildSelectedRepositories`. Fallbacks and empty diffs carry none.
  `validateGenerationInput` rejects a delta without a baseline and a full
  generation with one.
- `projector.DecideDeltaBaseline` is the one decision table. The enforced
  invariant is: a delta generation activates only while
  `active(scope).source_commit_sha == delta.delta_baseline_commit_sha`.
- `projector.PreflightDeltaBaseline` runs first in `Service.processWork` (the
  projector and ingester binaries) and in `drainProjectorWorkItem`
  (bootstrap-index, sequential and concurrent), before the heartbeat, the
  large-generation semaphore, and `LoadFacts`. The fence is a required, named
  dependency: `Service.validate` and `drainProjector` refuse a nil one, and a
  wiring test per binary fails when it is not wired.
- `ProjectorQueue.Ack` runs `deltaBaselineFenceQuery` as its own statement after
  the scope-row lock statement and the work-row update, before the first
  statement that changes another generation. A refusal rolls back, then marks
  the work row and generation `superseded` in one statement
  (`markProjectorDeltaBaselineRefusedQuery`, work row first, never the scope
  row, never demoting an active generation).
- The active-generation predicate is one constant,
  `activeGenerationPredicate`, shared with `lastProjectedCommitSHAQuery`.

Not fixed here: #7389. A generation can write the graph and never activate; a
later delta then passes this fence on a graph that is not at its baseline. An
Ack-phase refusal also leaves the refused overlay in the graph until a later
delta or the reconciliation sweep repairs it. The fence enforces the activation
invariant above; it does not guarantee the graph is at a delta's baseline.

## Proof

Commands run from `go/` against `postgres:18-alpine` (container `7319-pg`,
127.0.0.1:25719) with
`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN=postgres://postgres:proof@127.0.0.1:25719/proof?sslmode=disable`:

- RED on main, GREEN on the branch: the binding race above now returns
  `ErrWorkSuperseded` with `projector_delta_baseline_mismatch_after_projection`,
  gen-d `superseded`, gen-b `active`, pointer gen-b.
- `TestProjectorDeltaBaselineAckMatrix`: matched, active differs, no active,
  failed target with no active, target already active, legacy delta, full,
  full at the same commit activated in between, active back at A by SHA,
  baseline equal to a never-activated commit; every refusal row also proves a
  duplicate Ack is a claim rejection that moves nothing.
  `TestProjectorDeltaBaselineChain`: D1 then D2.
- `TestProjectorDeltaBaselineAckLeaseLostBeforeMark`,
  `TestProjectorDeltaBaselineMarkFailureConverges`,
  `TestProjectorDeltaBaselinePreflightLive`,
  `TestProjectorDeltaBaselineReadAndMarkNeverWaitOnScopeRow`.
- `TestProjectorDeltaBaselineConcurrentAcks` (eight same-scope Acks, mixed
  baselines, at most one activation per baseline, no 40P01) and
  `TestProjectorDeltaBaselineAckWaitsOutIngestionCommit` (lock order against an
  ingestion transaction holding the scope and generation rows), with the race
  test, under `-race -count=10`: pass.
- `TestMigration148AddColumnIsBoundedByTheRunner`: one attempt under a held
  ROW EXCLUSIVE lock fails with 55P03; the runner's retry waits it out; an Ack
  during the wait returns `ErrWorkAckDeferred`, then succeeds after the column
  exists.
- Every live test in `go/internal/storage/postgres` on the same DSN passes.

No-Regression Evidence: the fence read adds one statement to every Ack. On
PostgreSQL 18.6 with 800 scopes by 64 generations (51,200 rows, migrations 001,
002, 085 and 148), `EXPLAIN (ANALYZE, BUFFERS)` of `EXECUTE` under
`plan_cache_mode = force_generic_plan` is a nested loop of an index scan on
`scope_generations_pkey` (target) and an index scan on
`scope_generations_active_scope_idx` (active row), 6 shared buffer hits, no
sequential scan.

The first measurement (400 executions interleaved with the existing baseline
read on the same session, host load average 48 to 56) gave fence p50 0.024 ms,
p90 0.047 ms, p95 0.065 ms, p99 0.122 ms, above the arbiter's 0.1 ms bound.
That host was NON-PD (rule PD: load1 under half the CPU count), so the number
was not trusted. A quiet-host rerun on a 16-CPU host (load1 0.00 at start,
0.15 maximum during the run, 0.15 at end; migrations, seed and
`plan_cache_mode = force_generic_plan` unchanged), 2,000 executions: fence p50
0.042 ms, p95 0.056 ms, p99 0.072 ms, max 0.111 ms; the same-shape control
canary (the #7363 single-index read) p50 0.020 ms, p99 0.042 ms, inside its
known-good 0.011 to 0.057 ms range. The p99 clears the 0.1 ms bound without
excluding anything. The five executions above 0.1 ms were each a fresh
session's first `EXECUTE`, where the generic plan is built; excluding those,
p99 is 0.070 ms and max is 0.100 ms. Production connections are pooled
(`database/sql` plus pgx stdlib, `MaxOpenConns=30`, `ConnMaxLifetime=30m`), so
this cold-plan cost recurs per physical connection roughly every 30 minutes
and is amortized over many Acks on that connection; production's default
adaptive `plan_cache_mode` (not forced generic) means the real cost is at or
below this deliberately pessimistic measurement. No worker count, batch size,
lease, or lock order changed; the fence takes no lock, and the refusal mark
never takes the scope row.

Observability Evidence: `eshu_dp_projector_delta_baseline_fence_total` by
`phase` (`preflight`, `ack`) and `outcome` (`matched`, `unfenced`,
`already_active`, `refused_active_differs`, `refused_no_active`). The Ack unit
and live tests assert one `phase=ack` point per decision and none for a full
generation; the preflight live test asserts `phase=preflight
outcome=refused_active_differs`. Refusals log WARN (preflight) or ERROR (Ack)
with `delta_baseline_commit_sha`, `active_commit_sha`, `active_generation_id`,
`fence_phase`, and `failure_class`, and add a `projector.delta_baseline_refused`
event to `projector.run`. The work row carries the failure class and
`failure_details`. The collector's `git repository sync completed` log carries
`delta_baseline_commit_sha` for a delta.

## Audit query

Activated deltas whose recorded baseline is not the commit of the generation
activated before them (limited to generations retention has kept). It returns
0 rows on the 51,200-row fixture and returns the one row planted with a wrong
baseline:

```sql
WITH activated AS (
    SELECT scope_id, generation_id, is_delta, delta_baseline_commit_sha,
           lag(source_commit_sha) OVER (
               PARTITION BY scope_id ORDER BY activated_at, generation_id
           ) AS previous_active_commit
    FROM scope_generations
    WHERE activated_at IS NOT NULL
)
SELECT scope_id, generation_id, delta_baseline_commit_sha, previous_active_commit
FROM activated
WHERE is_delta
  AND delta_baseline_commit_sha IS NOT NULL
  AND delta_baseline_commit_sha IS DISTINCT FROM previous_active_commit;
```

## Not checked

- The explicit-mode Neo4j end-to-end scenario (graph, content store,
  `git ls-tree` equality, recovery latency in cycles, repeat with a
  reconciliation full) was prepared by the implementer and is run separately.
- The residual Ack-phase window (a second valid claim in one scope) is argued
  from the claim SQL, not enumerated.
- Retention of superseded generations limits the audit query's reach.
