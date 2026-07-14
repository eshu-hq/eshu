# Evidence — reducer executor-retry seam (#5048, #5086)

Records the performance and observability evidence for hoisting the reducer's
per-call `sourcecypher.RetryingExecutor` to a persistent startup-constructed
value and adding the "arm above, fire below" seam for the Ifá `executor-retry`
fault lane, so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record specific
to this change rather than relying on PR/commit prose.

## Change shape

- `cmd/reducer/neo4j_wiring.go` + new `cmd/reducer/reducer_executor_adapters.go`:
  the executor adapters (`reducerNeo4jExecutor`, `reducerCypherExecutor`,
  `cypherRunnerStatementExecutor`, the semantic-observed executor and its
  helpers) move out of `neo4j_wiring.go` (495 → 370 lines) into the sibling
  adapters file (500-line cap). `reducerNeo4jExecutor`/`reducerCypherExecutor`
  now hold a **persistent** `*RetryingExecutor` built once in
  `newReducerNeo4jExecutor`/`newReducerCypherExecutor`, replacing the deleted
  per-call `executeReducerCypherWithRetry` that rebuilt a fresh
  `RetryingExecutor` on every `Execute`.
- `internal/storage/cypher/fault_executor.go`: the `FaultingExecutor` gains an
  optional `ExecutorRetryArmer`; for the `executor-retry` lane it arms the armer
  and delegates through `inner.Execute` instead of returning the shaped error,
  so the induced failure reaches the persistent `RetryingExecutor` below the
  seam. #5086 extends the same arming path to `ExecuteGroup`; phase groups stay
  on their separate fallback path.
- `cmd/reducer/ifa_fault_wiring.go` (ifafaultinjection build tag only): swaps the
  persistent executor's `retry.Inner` for an armed decorator at startup. The
  decorator now implements both `Execute` and `ExecuteGroup`. Each fired fault
  is carried by a private derived context, so an unrelated concurrent writer
  cannot consume the one-shot failure intended for the matched call.
- `cmd/reducer/reducer_executor_adapters.go`: grouped reducer writes now call
  the persistent `RetryingExecutor.ExecuteGroup` instead of bypassing it through
  `session.RunCypherGroup`. Retryable immediate transient/connectivity failures
  may repeat a complete atomic group. Commit-ambiguous failures are not retried
  in place by `RetryingExecutor`; still-pending idempotent work may be replayed
  later by its durable owner after backoff. Malformed connectivity failures
  become safe terminal errors. Commit-time UNIQUE retry is narrower and
  requires every statement in the group to be MERGE-shaped.

## No-Regression Evidence:

- **Change shape:** #5048 changed production construction lifetime from
  per-call to persistent and wired `Instruments`. #5086 additionally routes
  production `ExecuteGroup` through that persistent executor. The
  `ifafaultinjection` armer remains absent from untagged builds.
- **Baseline / after:** the retry cadence and bounded outer budget are shared
  by single and grouped execution. #5086 also prevents a typed driver
  `TransactionExecutionLimit` from multiplying the driver's already-exhausted
  inner budget.
  Before #5086, a commit-time uniqueness collision from
  `reducerNeo4jExecutor.ExecuteGroup` returned after one failed group attempt.
  After, the focused reproduction records two attempts (one failed commit and
  one successful retry) and returns success. Non-MERGE groups remain ineligible
  for commit-time UNIQUE replay in
  `TestRetryingExecutorExecuteGroupDoesNotRetryNonMergeStatements`.
- **Backend/version:** no Cypher text, batch size, production worker knob,
  lease, or `statement_ordinal` semantics changed. The retry classification is
  the existing NornicDB commit-time UNIQUE classifier. The ordinal is still
  incremented once per top-level `FaultingExecutor.Execute` or `ExecuteGroup`
  call and does not advance across retry attempts.
- **Input shape / terminal counts:** the composition root keeps the same queue,
  projection, statement, and transaction-group shapes. Successful first
  attempts are unchanged; the intentional behavior delta is bounded in-place
  recovery for classified grouped failures. `go test ./cmd/reducer/...
  ./internal/storage/cypher/... -count=1` passes untagged and with
  `-tags ifafaultinjection`, and `-race -tags ifafaultinjection` is clean.
- **Why safe:** grouped replay is bounded and commit-time UNIQUE recovery is
  restricted to all-MERGE groups; the new failing-first reducer test
  `TestWrapIfaFaultExecutorExecutorRetryLaneRetriesInPlaceBelowTheRetryingExecutor`
  proves the executor-retry lane now retries in place (one real session write
  after one absorbed retry), which the old above-the-seam structure could not
  satisfy.
- **#5086 group proof:**
  `TestReducerNeo4jExecutorRetriesMergeGroupCommitConflict` failed before the
  adapter used `RetryingExecutor.ExecuteGroup`, then passed with two group
  attempts. Under `-tags ifafaultinjection`,
  `TestWrapIfaFaultExecutorGroupRetryLaneRetriesInPlaceBelowTheRetryingExecutor`
  failed above the retry seam, then passed with exactly one real session write
  after the induced first attempt was absorbed below the seam.
- **Pinned live-backend non-vacuity:**
  `TestReducerGroupedRetrySeamLiveNornicDB` runs the production composition on
  NornicDB commit `1492458852588c884c32f70d27ea2ee07086769c`. In 10/10 runs it
  asserts the group fault fired, the retry counter equals one, and both MERGE
  statements persisted exactly once.
- **Retry-budget boundary:**
  `TestRetryingExecutorExecuteGroupDoesNotRetryDriverBudgetExhaustion` uses the
  driver's typed `TransactionExecutionLimit` with a final transient deadlock.
  Before the narrow classifier guard, the outer executor made four group calls
  after the driver budget was already exhausted; after, it returns the typed
  exhaustion after one call. The immediate commit-time MERGE UNIQUE regression
  still makes two calls and succeeds, preserving #5086's required retry.
- **Unknown commit outcome boundary:**
  `TestRetryingExecutorExecuteGroupDoesNotRetryCommitFailedDeadConnectivityError`
  reconstructs the driver's public `ConnectivityError` wrapper around the
  private `CommitFailedDeadError` shape. Before the guard, the outer executor
  replayed the group four times; after, it preserves the error after one call.
  This matches the Neo4j driver's own non-retryable classification: a lost
  connection during commit does not prove whether the transaction landed.
  This is an in-place executor boundary, not a durable-queue terminality claim.
  Repo-dependency work remains pending and may replay after backoff; its
  source-scoped retract and deterministic MERGE upserts are idempotent.
- **Concurrent fault identity:**
  `TestIfaExecutorRetryArmedExecutorBindsExecuteFaultToArmingContext` and
  `TestIfaExecutorRetryArmedExecutorBindsGroupFaultToArmingContext` run the
  targeted call beside unrelated callers and prove only the targeted context
  consumes the fault, exactly once, under the race detector.

## Observability Evidence:

This change is not observability-neutral: the deleted per-call
`executeReducerCypherWithRetry` constructed its `RetryingExecutor` **without**
`Instruments`, so the reducer never recorded the `Neo4jDeadlockRetries` counter
(`internal/storage/cypher/retrying_executor.go`) on graph-write retries — the
only recording site in the codebase. `newReducerNeo4jExecutor` /
`newReducerCypherExecutor` now thread `Instruments` from
`openReducerNeo4jAdapters`, so reducer graph-write deadlock/transient retries are
now counted for operators. No metric, span, or log name was renamed or removed;
this only starts emitting an already-defined counter from the reducer that was
previously silently dropped.

For #5086 grouped writes, the existing `Neo4jDeadlockRetries` counter is now
also reached by `RetryingExecutor.ExecuteGroup`. Its metric name is unchanged;
each retry now carries the bounded `reason` label (`connectivity_error`,
`transient_error`, `write_conflict`, or `commit_unique_conflict`) beside
`write_phase`. `TestRetryingExecutorRetryMetricUsesBoundedReason` proves raw
repository/node/error data does not enter the metric attributes. No new span or
log field is introduced. The proof-only repo-dependency worker coordinator is
compiled only with `ifarepodependencyproof`, so normal builds retain the global
0/1 lane and its existing repo-dependency cycle/step/lease telemetry.
