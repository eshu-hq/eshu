# Corpus Fence And Foreign Read Share One Snapshot

Issue #6740. Follow-up to PR #6730, which re-checked the corpus fence after the
by-repos resolved-relationship read. The fence and the read were still
separate statements, so one interleaving got through: a foreign scope retires
its relationship generation after the fence passes, the read misses that
scope's rows, the scope re-activates, and the recheck passes too. Workload
materialization or deployable-unit correlation then succeeds on a partial
foreign set, and nothing reopens it.

## Fix

`RelationshipStore.GetResolvedRelationshipsForReposWithCorpusFence` runs the
fence predicate (`incompleteScopeRelationshipGenerationsPredicate`, the same
constant the boolean and holder queries use) in a `MATERIALIZED` single-row
CTE and LEFT JOINs the by-repos read to it. One READ COMMITTED statement uses
one snapshot, so the verdict and the rows describe the same committed state.
The reducer's optional `CorpusFencedResolvedRelationshipLoader` interface is
preferred by `readCorpusFencedResolvedRelationships`, which both the workload
projection input loader and the deployable-unit correlation handler call. On
that path the separate fence lookup is not called at all. On the complete
path the pre-read check, the read, and the recheck (3 statements) become 1.
On the deferral path the old code ran 1 fence-only statement and the new code
runs 1 fused statement, which is slightly more expensive (see Plans). The
own-scope read runs only after a complete verdict, so a deferral never pays
for it. Stores without the method
keep the #6730 two-statement fence and recheck. A test in
`go/internal/storage/postgres` asserts the production store implements the
interface.

The own-scope read is pinned to the intent's active generation, whose rows do
not change after activation, so it is not coupled to the fence.

## Theory proof (throwaway shim, before any production code)

Laptop-local: OrbStack Postgres 18.6 (`eshuorch-pg`), a throwaway database
built from `pg_dump -s` of the five tables in the migrated schema, and the
fence predicate copied verbatim from the shipped constant.

- Session 1 ran the fused SELECT with `pg_sleep(3)` placed in the fence CTE
  ahead of the predicate. It started at 12:45:37.65. At 12:45:38.75, session 2
  committed an advance-and-complete of scope-x: `active_generation_id` moved
  from x1 to x2, x1 was superseded, and x2 was activated with its row. Session
  1 finished at 12:45:40.69 with `complete=t` and only the old rows, `x1/old-x1`
  and `y1`. Re-run after the commit, it returned `complete=t` with
  `x2/new-x2` and `y1`.
- Retire only (x1 set to `pending`) committed during the sleep: session 1
  returned `complete=t` with the old rows. Re-run, it returned `complete=f`
  and no rows.
- Negative control, today's three statements in sequence: fence `t`, then x1
  is retired, then the read returns `y1` only (scope-x missing), then x2
  activates, then the recheck returns `t`. This is the #6740 residual.

## Regression tests

- Reducer (RED before the fix, rc=1): a stub store whose fused read returns
  partial rows with `complete=false`, while `ResolutionsCompleteLookup`
  returns true on every call. Before the fix both loaders ignored the fused
  verdict and succeeded (`error = nil`). After the fix they defer retryably,
  report the holding scopes, and make no unfenced by-repos call.
  `go/internal/reducer/resolved_relationships_corpus_fence_test.go`.
- Reducer, review F1 (RED on 550a83968, rc=1, `own-scope read calls = 1,
  want 0 on a deferral`): an incomplete fused verdict must not run the
  own-scope read in either consumer.
  `TestWorkloadProjectionInputsSkipOwnReadWhenFencedReadDefers` and
  `TestDeployableUnitCorrelationHandleSkipsOwnReadWhenFencedReadDefers`.
  Companion tests cover deployable-unit fused success and fused read error,
  and a zero-candidate scope that runs neither the fused read nor the lookup.
- Store, live Postgres, isolated schema per test
  (`go/internal/storage/postgres/resolved_corpus_fence_snapshot_live_test.go`):
  - An uncommitted advance-and-complete in another session is invisible. The
    read returns `complete=true` with the old rows. After the commit it returns
    `complete=true` with the new rows. After a retirement it returns
    `complete=false` and no rows.
  - Deterministic interleaving: a query wrapper commits the retire after the
    method's first statement and the re-activate after its second. The fused
    method issues one statement and returns both scopes' rows. With the method
    swapped for a fence/read/recheck composition (a temporary mutation), the
    test fails with `fused read = ([y-v1], complete=true)`, the exact residual.
  - A concurrent run: 200 retire/re-activate writer cycles against two readers.
    One recorded run had 1272 complete reads and 1966 incomplete reads, with
    0 violations. This test did NOT catch the mutation (the gap is one round
    trip wide). The deterministic test above is the one that shows it matters.

## Plans

No-Regression Evidence: laptop-local EXPLAIN (ANALYZE, BUFFERS), same shim
server, seeded with 5000 active scopes, 5000 scope generations, 10000
relationship generations (one superseded and one active per scope), 200000
resolved rows, and 20000 succeeded work items. 10 repository ids, warm cache,
three discarded warmups per statement.

| Statement | Execution | Shared buffers hit |
| --- | --- | --- |
| fence alone | 2.826 ms | 272 |
| by-repos read alone | 1.473 ms | 362 |
| fused (fence complete) | 4.009 ms | 634 |
| fused (fence incomplete) | 3.568 ms | 597 |

The fused plan is the two existing plans side by side: a seq scan of
`ingestion_scopes` with hashed relationship-generation subplans in the fence
CTE, then a nested-loop left join over a bitmap-OR of
`resolved_relationships_source_repo_idx` and
`resolved_relationships_target_repo_idx`. Its buffers equal the fence's plus
the read's (272 + 362 = 634). Before the fix, a pass ran fence, read, and
recheck: about 7.1 ms of execution in 3 round trips. The fused path is about
4.0 ms in 1. That comparison holds only on the complete path.

The deferral path costs more than before. When the fence is incomplete, the
fused statement still runs the inner read and the join filter drops it (400
rows removed): 3.568 ms and 597 buffers, from the shim artifact
explain_fused_false.out. The old code deferred on its pre-read fence alone,
2.826 ms and 272 buffers, with no read. So each deferral now costs about
0.7 ms and 325 buffers more on this seed, and a deferring intent re-polls
until the corpus settles. The old code paid the fused-false cost only when its
post-read recheck flipped. A LATERAL `WHERE fence.complete` gate and a
scalar-subquery gate were both tried, and the planner produced the same plan
for each, so the plain `LEFT JOIN ... ON fence.complete` shape was kept.

Remote-seed EXPLAIN: NOT_CHECKED. These numbers are laptop-local.

## Concurrency

The conflict domain is `relationship_generations.status` and
`ingestion_scopes.active_generation_id` for foreign scopes, which
deployment_mapping resolution writes while workload materialization and
deployable-unit correlation read them. The fix takes no locks, starts no
transaction, and holds no long snapshot: the statement snapshot lasts as long
as the one statement, so N workers add no vacuum-horizon pressure. Worker and
lease settings are unchanged, and nothing is serialized.

## Observability

Observability Evidence: the change reuses the existing signals. An incomplete
fused verdict returns the same `workloadMaterializationResolutionNotReadyError`
or `deployableUnitCorrelationResolutionNotReadyError` as before, with the same
non-counting retry failure class. It still names the holding scopes through
`IncompleteScopesLookup`, so the queue's recorded failure class and the
deferral error text look exactly as they did for a two-statement deferral.
No metric, span, or log key was added.

Failure-class change: on the old path a fence lookup error (Postgres
unreachable, for example) became a non-counting deferral that carried the
cause (#6730). On the fused path the fence and the read are one query, so the
same error is a plain read error, `list resolved by repos with corpus fence:
...`, which counts against the intent's attempts like any by-repos read error
always did. A long enough outage can therefore dead-letter a workload or
deployable-unit intent that the old path would only have deferred.
`TestWorkloadProjectionInputsFailOnFencedReadError` and
`TestDeployableUnitCorrelationHandleFailsOnFencedReadError` pin this.

## Follow-up

`servicecatalog.RepositoryScopedResolvedRelationshipLoader`
(`go/internal/reducer/servicecatalog/service_catalog_correlation_ports.go`) reads
by repos with no corpus fence at all. This change does not cover it.
