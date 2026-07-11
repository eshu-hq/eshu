# Evidence — reducer executor-retry fault seam (#5048)

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
  seam. `ExecuteGroup`/`ExecutePhaseGroup` pass `allowArm=false` and keep
  returning the shaped error (groups bypass `RetryingExecutor`).
- `cmd/reducer/ifa_fault_wiring.go` (ifafaultinjection build tag only): swaps the
  persistent executor's `retry.Inner` for an armed decorator at startup.

## No-Regression Evidence:

- **Change shape:** production (non-`ifafaultinjection`) builds only change
  `RetryingExecutor` construction lifetime (per-call → persistent) plus wiring
  its `Instruments`. `RetryingExecutor` is stateless config (`Inner`,
  `MaxRetries`, `BaseDelay`, `Instruments`; all retry state is function-local in
  `runWithRetry`), so persistent vs per-call construction yields identical retry
  behavior. The armer field is nil in untagged builds and gated by
  `allowArm && fe.executorRetryArm != nil`, a no-op.
- **Baseline / after:** the retry classify/backoff algorithm is byte-identical;
  the only functional delta is one fewer per-call `RetryingExecutor` allocation
  on the reducer graph-write path and the now-recorded deadlock-retry counter.
- **Backend/version:** no Cypher shape, batch size, worker knob, lease, graph
  backend interaction, or `statement_ordinal` semantics changed. Verified on
  NornicDB (default backend) via the reducer + cypher suites; the ordinal is
  still incremented once per top-level `FaultingExecutor.Execute` call and does
  not advance across `RetryingExecutor` retry attempts.
- **Input shape / terminal counts:** the composition root wires the same
  adapters with the same constructor arguments; queue claim/drain, projection,
  and graph-write behavior are unchanged. `go test ./cmd/reducer/...
  ./internal/storage/cypher/... -count=1` passes untagged and with
  `-tags ifafaultinjection`, and `-race -tags ifafaultinjection` is clean.
- **Why safe:** a persistent-vs-per-call construction change on stateless config
  cannot regress throughput or correctness; the new failing-first reducer test
  `TestWrapIfaFaultExecutorExecutorRetryLaneRetriesInPlaceBelowTheRetryingExecutor`
  proves the executor-retry lane now retries in place (one real session write
  after one absorbed retry), which the old above-the-seam structure could not
  satisfy.

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
