# Code-Quiescence Gate Scope

Issue #7133. On ops-qa (Neo4j, image `sha-fb08c0c`), the `code_calls` and
`repo_dependency` shared-projection lanes stopped draining at 2026-09-17
23:03Z. `code_calls` stood at 1,526,208 pending intents in three snapshots
between 15:04 and 15:10Z on 2026-09-25, a drain rate of 0/s. Neo4j held 0
`CALLS` edges and 536,264 `Function` nodes.

Root-Cause Evidence: the canonical-code quiescence probe
(`uncommittedCanonicalCodeScopesQuery`, `go/internal/storage/postgres/reducer_graph_drain.go`)
is one lane-wide boolean that both runners check before claiming a lease.
Its second branch holds any active generation with zero facts and no
`code_entities_uid/canonical_nodes_committed` phase row. It did not restrict
itself to scopes that can ever publish that phase. Two active scopes held it
true on ops-qa: `aws:<account>:us-east-1:ecs` (source_system `aws`, kind
`region`, 17 generations since 2026-09-17 23:03:48Z, all with 0 facts) and the
synthetic `eshu:global` scope from migration 115, whose migration-116 phase
row was missing. Read-only counterfactual on ops-qa: the full predicate
returned `true`, and the same predicate with those two scope_ids excluded
returned `false`. No git scope was uncommitted (branch 1 returned 0 rows
across 794 git scopes). The last `code_calls` completion (23:03:21Z) came 27 s
before the first empty ECS generation.

## Fix

- The probe now admits only scopes whose collector contract requires the
  `code_entities_uid` canonical-nodes phase: `scope.collector_kind =
  ANY($3::text[])`. The set comes from
  `workflow.CollectorKindsRequiringPhase` (today `{git}`), not a hand-kept
  list. The probe also excludes `scope.scope_kind <> 'repository_ref'`,
  because the projector returns before any canonical write for
  non-default-branch ref scopes (`projector/runtime/projection.go`). Before
  this change, such a scope held the lane forever too.
- The #6184 guarantee holds. Every scope that can own a MATCHed Repository or
  code node is a git default-branch scope, and it still holds the lane while
  its repository facts lack a phase row, or while it has committed no facts
  yet.
- Migration 116's `eshu:global` seed row no longer matters to this gate:
  `eshu:global` (collector `reducer`) is never eligible. The applied migration
  is unchanged. Whether rebuild/reset should keep synthetic-scope phase rows
  (issue point 4) is left for that issue. It no longer affects this gate.
- `deployable_unit_correlation` calls the same probe
  (`deployableUnitCanonicalReposReady`), so its non-counting
  canonical-nodes-not-ready defers (486 retrying on ops-qa) clear with the
  same fix.

Seeded RED/GREEN (postgres:16 container, `ApplyBootstrap` schema):

```
ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:25473/eshu \
  go test ./internal/storage/postgres -run 'ReducerGraphDrainQuiescence' -count=1 -v
```

- Base (predicate unchanged, new tests added): exit 1.
  `TestReducerGraphDrainQuiescenceIgnoresNonCodeScopes`: "zero-fact aws region
  scope and phase-less eshu:global must not hold the lane: ... = true, want
  false". `...IgnoresRefScopes` failed the same way.
  `...StillHoldsGitScopes` failed at its release step, because the AWS scope
  still held the lane. The #6184 `...IsPerRepository` proof passed.
- Head: exit 0 for all four. The same run on a local PostgreSQL 18 cluster
  also passed.

Each new clause is independently necessary. The AWS case is excluded only by
the collector filter, and the ref case (collector `git`) only by the
scope-kind filter. Both failed on the base.

No-Regression Evidence: EXPLAIN (ANALYZE, BUFFERS), 7 interleaved runs per
query, medians. The run used a postgres:16.15 container with the
`ApplyBootstrap` schema. The fixture has 801 scopes (794 git, 5 AWS region
with zero-fact active generations, 1 Terraform state, `eshu:global`), 20,001
generations, 501,064 facts (151 per active git generation, 20 per superseded
one) and 7,147 phase rows. Query text was extracted verbatim from the base and
head constants.

| State | base probe | head probe | head blocker sample |
| --- | --- | --- | --- |
| ops-qa shape (AWS zero-fact + eshu:global phase-less) | 1.64 ms, 144 blocks, **true** | 63.2 ms, 131,116 blocks, false | 63.3 ms |
| steady (all committed) | 60.8 ms, 131,285 blocks, false | 60.6 ms, 131,117 blocks, false | 63.6 ms |
| one git scope uncommitted (last in scan order) | 60.4 ms, 131,255 blocks, true | 58.5 ms, 131,112 blocks, true | 65.7 ms |

A local PostgreSQL 18.6 cluster gave the same picture (steady: base 74.0 ms,
head 72.0 ms, about 129.6k blocks each). The comparable steady and uncommitted
states run at the same cost or slightly lower: the collector filter drops
non-code scopes before either subquery runs. The base's 1.64 ms in the ops-qa
shape is the wedge itself, a short-circuit on the wrong answer. The blocker
sample is not on the per-cycle path. It runs only when a blocked episode
starts and at most once a minute after that.

Pre-existing cost, unchanged by this PR, flagged for a follow-up issue:
SubPlan 1 reads `fact_records_scope_generation_keyset_idx (scope_id,
generation_id, observed_at, fact_id)` and heap-filters `fact_kind` over every
fact of every active git generation ("Rows Removed by Filter: 150" per scope).
That comes to about one buffer per active git fact per call (119,894 active
git facts in the fixture). A rolled-back shim that dropped that index made the
planner use `fact_records_collector_status_active_idx`: 15.8 ms median over 5
runs and about 9.5k buffers on postgres:16. The base plan is identical, so
this is not a regression. On ops-qa the per-call cost scales with active git
fact count. It needs its own proof on ops-qa-shaped data (partial index or
extended statistics) before any change.

ops-qa measurement of this PR's predicate (read-only `EXPLAIN (ANALYZE,
BUFFERS)` through a `default_transaction_read_only` session, 2026-09-25,
78,614,620 `fact_records` rows, 794 active git scopes): the gate returns
false, so the lane unblocks. Three warm runs took 166.5, 167.3 and 170.5 ms
(19,095 shared buffers each); the first, cold run took 1,040 ms. SubPlan 1 and
SubPlan 2 run 794 times each, both through indexes
(`fact_records_collector_status_active_idx`, `fact_records_scope_generation_idx`,
`graph_projection_phase_state_pkey`); the uncorrelated SubPlans never execute,
so there is no whole-table `fact_records` scan. The wedged base
short-circuited in about 1.6 ms, so each code_calls and repo_dependency cycle
now pays about 170 ms for the gate. The predicate cost itself is unchanged
from a healthy base (see the fixture table above); making it cheaper is #7166.

## Observability

Observability Evidence: before #7133, a blocked `code_calls` cycle recorded
nothing (`recordCodeCallTiming` skips zero waits) and logged nothing. New
signals:

- `eshu_dp_shared_projection_lane_blocked_total{domain,reason}`: every
  blocked partition cycle for `code_calls`, and every quiescence-blocked cycle
  for `repo_dependency`. The `reason` values are
  `canonical_code_quiescence` and `reducer_graph_work_active`.
- `eshu_dp_shared_projection_lane_blocking_scopes{domain,reason}`: the
  blocking scope count, sampled per episode and zeroed on release.
- WARN `code call projection lane blocked`, with keys `blocked_reason`,
  `blocked_seconds`, `blocking_scope_count` and `blocking_scope_ids` (sorted,
  capped at 10). It fires when an episode starts, at most once a minute after
  that, and when the reason changes. INFO `code call projection lane released`
  closes the episode.

Scope ids appear only in logs, never as labels, so cardinality stays bounded
by two domains and two reasons. On ops-qa the line would have named
`aws:<account>:us-east-1:ecs` and `eshu:global`. Proof:
`TestCodeCallProjectionRunnerReportsQuiescenceBlock` (a mutation that drops
the record call fails it), `...ReportsReducerGraphWorkBlock`,
`...DescribeErrorKeepsCycleBlocked`,
`TestLaneBlockStateReportsOncePerIntervalAndOnRelease`,
`TestRepoDependencyProjectionRunnerRecordsQuiescenceBlockedCycle`, and
`TestCanonicalQuiescenceCheckerNamesItsBlockers`. The last one pins that the
production `postgres.ReducerGraphDrain` satisfies the optional describer
port.

## Concurrency

The gate is a read-only probe with no lock and no lease. The runner state is
one mutex-guarded episode record (`laneBlockState`) shared by the concurrent
partition workers. It serializes only the report decision, never gate
evaluation or lease claims. Worker counts, partition counts, lease TTLs and
batch sizes are unchanged. `go test -race` passed on
`internal/reducer/code/call/projection`, `internal/reducer`,
`internal/storage/postgres`, `internal/workflow`, `internal/telemetry` and
`cmd/reducer`.

## Backlog before unblock (not in this PR)

About 1.17M pending `code_calls` intents sit on non-active generations. The
code-call runner has no superseded-generation handling:
`worker.FilterAuthoritativeIntents` keys acceptance by `source_run`, and every
pending row has an acceptance row equal to its own generation. Candidates are
ordered `is_refresh_intent DESC, created_at ASC`, so after the unblock those
rows replay oldest-first as real retract/write cycles. The #7121 fix
(PR #7159) adds a
`SupersededGenerationReader` port only to `worker.SelectPartitionBatch`. The
code-call runner (`code/call/projection/selection.go`) and the repo-dependency
runner select without it. The proposal:

1. Do not drain every row of a superseded generation. A delta generation
   carries only changed-file facts, so when the successor is a delta the
   superseded full generation's rows are the only source of the unchanged
   files' CALLS edges. Draining them loses those edges with no error. A drain
   must keep the rows of the newest full generation and everything after it,
   using the safety conditions #7121 (PR #7159) settled on. `superseded` is
   also not terminal: projector Ack can reactivate it (#7130). A first cut of
   this drain that missed both points was dropped from this PR in review;
   the delta-safe drain is #7165.
2. `repo_dependency` rows carry resolver relationship-generation ids, not
   scope generation ids, so the #7121 lookup does not match them. Its backlog
   is 9,737 rows. Measure replay cost first, and design a
   relationship-generation to scope-generation mapping only if that cost
   matters.
3. Measure the drain on a scaled fixture before and after, then unblock
   ops-qa.

## Follow-up: blocked-lane sampler precedence and episode close

The #7179 review threads changed only the blocked-lane reporting in
`go/internal/reducer/code/call/projection/blocked.go`. The gate query, its
arguments, the partition loop and the claim path are untouched.

No-Regression Evidence: the per-cycle work is unchanged. The sampler still
calls the blocker describer only when an episode starts, when the reason
changes, and at most once a minute after that; it now asks the same dependency
the gate consulted instead of a fixed one. A reason switch adds one INFO log
line for the replaced episode (zeroing its gauge already happened before this
change), so the added cost is bounded by the switch rate, the same order as the
existing blocked WARN. `go test ./internal/reducer/...` passes.

Observability Evidence: a reason switch now logs `code call projection lane
released` for the replaced reason with its `blocked_seconds` age, so stall time
has a close record per reason. `blocking_scope_ids` always comes from the
dependency that holds the lane. Tests: `TestQuiescenceDescriberMirrorsGatePrecedence`,
`TestQuiescenceDescriberDoesNotBorrowFromANonGateDependency`,
`TestCodeCallProjectionRunnerLogsClosedEpisodeOnReasonSwitch` (RED before the
change) and `TestCodeCallProjectionRunnerNonDescribingGateStillReportsBlock`.
