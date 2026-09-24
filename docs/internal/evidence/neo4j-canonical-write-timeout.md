# Neo4j canonical write timeout

## Observed failure

ops-qa is moving its graph backend from NornicDB to Neo4j. On Neo4j,
`ESHU_CANONICAL_WRITE_TIMEOUT` had no effect: `canonicalTransactionTimeout`
(ingester), `reducerTransactionTimeout` (reducer),
`bootstrapCanonicalTransactionTimeout` (bootstrap-index), and
`projectorCanonicalTransactionTimeout` (projector) each returned `0` for any
backend other than NornicDB. Every write session already passed
`neo4j.WithTxTimeout` through `transactionConfigurers()`, but with a zero value
the driver sends no timeout, so a hung Neo4j write ran unbounded while
lease-guarded work (for example `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL=8m`)
assumed a bounded write.

## Change

- Neo4j now applies `ESHU_CANONICAL_WRITE_TIMEOUT` as the server transaction
  timeout when it is set to a positive duration. Unset or invalid values keep
  Neo4j unbounded, so a deployment that never configured the budget does not
  inherit NornicDB's `30s` default. NornicDB behavior is unchanged.
- The retry classifier treats the three statuses Neo4j reports for a
  timed-out transaction the same way it already treated NornicDB's
  `TransactionTimedOutClientConfiguration`: no local retry, a durable
  `graph_write_timeout` deferral for the replay-safe `RUNS_ON` groups only, and
  terminal elsewhere.

## Classifier evidence

- `Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration`: the
  status Neo4j uses for a client-supplied timeout (the timeout-taking
  `KernelImpl.beginTransaction` overload, Neo4j `2026.06.0`).
  NornicDB reuses this status verbatim, so this case already matched before
  the change; the new tests pin it for Neo4j.
- `Neo.ClientError.Transaction.TransactionTimedOut`: the status for the
  server-wide `db.transaction.timeout` (the overload without a timeout
  argument). Both statuses come from the same `TransactionTimeout`; only the
  status differs.
- `Neo.ClientError.Transaction.LockClientStopped`: observed live. A write that
  is waiting on another transaction's lock when its timeout fires does not
  report a timeout status. Terminating a transaction stops its lock client
  (`KernelTransactionImplementation.markForTerminationIfPossible` calls
  `lockClient.stop()`), and the pending lock acquisition fails with this
  status.

Rollback safety: `closeTransaction` checks `canCommit()` (commit requested and
no termination mark) before `commitTransaction`, so a transaction terminated
before commit is rolled back, and the terminating status reaches the client
through `failOnNonExplicitRollbackIfNeeded` or through the failed statement.
The live test below confirms that neither shape left a committed contender
write.

Before this change, `LockClientStopped` matched the transient `"LockClient"`
substring in `classifyTransientNeo4jError`, so `RetryingExecutor` retried it
in place up to three more times. Each retry waits one more full timeout while
the caller still holds its lease. On ops-qa, with a 300s timeout, that is up
to 20 minutes for one write. `isTransactionTimedOut` now matches the exact
typed status before the substring check. Typed-code matching only: the
`Neo.TransientError.*` spellings, strings without a typed code, and code
prefixes stay on their existing paths
(`TestNeo4jTransactionTimeoutClassificationFailsClosedForLookalikes`).
NornicDB emits none of the new codes (checked in NornicDB `pkg/` source), so
its classification is unchanged.

## Lease-TTL findings

- The repo-dependency runner validates at startup that
  `LeaseTTL > CycleTimeout + GraphQuiescenceBudget + 30s`. The budget is
  `nornicDBCanonicalWriteTimeout(getenv)` on both backends, meaning the env
  value or `30s`. ops-qa (`8m` lease, `45s` cycle, `300s` write) passes with
  `375s < 480s`. The runner also renews its lease with a heartbeat.
- Code-call projection renews its lease with a heartbeat.
- The value-flow stale cleanup (`5m`) and graph orphan sweep (`5m`) leases have
  no heartbeat and no startup check against the write timeout. With a 300s
  write timeout their TTL equals the write budget. This change does not add a
  startup gate there, because such a gate would fail ops-qa's current
  configuration at boot. It is left for the owner to decide.
- With `ESHU_CANONICAL_WRITE_TIMEOUT` unset on Neo4j, writes stay unbounded,
  while the repo-dependency check still counts a `30s` budget. That gap existed
  before this change and remains until the variable is set.

## Local proof

RED before the change, GREEN after:

- `Test{Canonical,Reducer,BootstrapCanonical,ProjectorCanonical}TransactionTimeoutAppliesConfiguredTimeoutToBothBackends`:
  on origin/main, Neo4j with `300s` returned `0s`. After the change, all seven
  cases pass for each of the four packages, including the unchanged NornicDB
  default and invalid-value fallbacks.
- `TestRetryingExecutorDefersNeo4jTransactionTimeoutForReplaySafeGroup`: with
  only the helper renames applied, the `TransactionTimedOut` case failed. With
  the `LockClientStopped` case added, the lock-wait case failed on both the
  deferral and the terminal tests, because the substring path retried it
  locally.
- `TestComposeForwardsGraphWriteBoundsToEveryGraphWriter`: run against
  origin/main's `docker-compose.neo4j.yml`, it failed with
  `compose env ESHU_CANONICAL_WRITE_TIMEOUT missing`.
- `TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite`, run against
  `neo4j:2026-community` with `ESHU_CANONICAL_WRITE_TIMEOUT=2s` through the
  production `newReducerNeo4jExecutor` seam:
  - lock wait: aborted after 3.47s with `LockClientStopped`, one attempt,
    terminal, holder value intact.
  - long statement: aborted after 2.27s with
    `TransactionTimedOutClientConfiguration`, one attempt, terminal, seed
    value intact.
  - Mutation with `LockClientStopped` removed from the classifier: 4 attempts,
    11.69s, failed `want 1`.
  - Mutation with the Neo4j gate returning `0`: failed
    `reducerTransactionTimeout(neo4j) = 0s, want 2s`.

## No-Regression Evidence:

The success path of `RetryingExecutor` does not run the changed classifier,
and the timeout wiring is evaluated once at startup. As a check,
`BenchmarkRetryingExecutorExecuteGroupSuccess5767` was run five times,
alternating origin/main and head on one shared Apple M5 Max host:

- base: 22.65, 45.14, 25.95, 23.69, 22.57 ns/op (median 23.69)
- head: 23.88, 34.13, 26.79, 26.27, 19.34 ns/op (median 26.27)
- both: 64 B/op, 1 alloc/op

The ranges overlap and the gap is host noise on code this change does not
touch. These figures do not estimate Neo4j throughput. On Neo4j with the
variable set, a write that previously would have hung now ends at the
configured timeout. That is the intended behavior change, and it is bounded
by the operator's own setting.

## No-Observability-Change:

No new metric, span, or log key. A Neo4j timeout surfaces through existing
signals. On writers wrapped by `InstrumentedExecutor` (ingester, bootstrap-index,
projector), the write span records error status with the typed code.
A replay-safe `RUNS_ON` group's queue row is recorded as retrying with
`failure_class=graph_write_timeout`, which feeds the existing
`eshu_dp_reducer_retry_surge_total` and the write-timeout backpressure depth
query. Terminal rows keep the typed code in `failure_details`. Because a
timeout is no longer retried in place, `eshu_dp_neo4j_deadlock_retries_total`
no longer counts `reason=transient` for a lock-wait timeout.
