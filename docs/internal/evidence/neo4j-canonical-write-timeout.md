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
- With the variable unset on Neo4j, each graph-writing binary (ingester,
  reducer, projector, bootstrap-index) logs one
  `graph.write_timeout.unbounded` WARN at startup, with `graph_backend` and
  `env_var`.
- Retry parity: where NornicDB bounds a write with `TimeoutExecutor`, Neo4j
  now gets the same wrapper when its timeout is set. Those paths are the
  ingester, bootstrap-index, and projector canonical chains and the reducer
  semantic-entity chain. The client deadline fires at the configured timeout,
  before the server terminates the transaction (the lock-wait case returned
  after 2.57 to 3.47s against a 2s timeout), so a timed-out write requeues as
  retryable `graph_write_timeout` on both backends instead of dead-lettering
  on Neo4j. With the variable unset, no client-deadline wrapper is added.
- The retry classifier treats the two statuses Neo4j reports for a timed-out
  transaction the same way it already treated NornicDB's
  `TransactionTimedOutClientConfiguration`: no local retry, a durable
  `graph_write_timeout` deferral for the replay-safe `RUNS_ON` groups only, and
  terminal elsewhere.
- `LockClientStopped` is handled separately, because Neo4j reports it for any
  transaction terminated during a lock wait: a timeout, an operator's
  `TERMINATE TRANSACTIONS`, or a database shutdown. It is no longer retried in
  place, and it returns to the durable queue as retryable
  `graph_write_timeout` in every group. Before this change it reached the same
  retryable end state only after three in-place retries. This classifier
  change applies on every Neo4j deployment, whether or not
  `ESHU_CANONICAL_WRITE_TIMEOUT` is set.

## Timeout gates enumerated

`rg -n "TimeoutExecutor\{|nornicDBCanonicalWriteTimeout\(|TransactionTimeout\(" go/cmd go/internal`
lists every NornicDB-only write-timeout gate, and each one is now handled:

- Server transaction timeout: `canonicalTransactionTimeout` (ingester),
  `reducerTransactionTimeout` (reducer), `bootstrapCanonicalTransactionTimeout`
  (bootstrap-index), `projectorCanonicalTransactionTimeout` (projector).
- Client deadline: `canonicalExecutorForGraphBackend` (ingester),
  `bootstrapCanonicalExecutorForGraphBackend` (bootstrap-index),
  `projectorCanonicalExecutorForGraphBackend` (projector),
  `semanticEntityExecutorForGraphBackend` (reducer; its caller in `main.go` now
  passes `reducerTransactionTimeout`).
- NornicDB-only by design, with no Neo4j counterpart: the ingester and
  projector timeout drain readers, which bound NornicDB's bounded
  `DETACH DELETE` drain loop, a loop Neo4j does not run.
- Out of scope: `bootstrap-data-plane`'s `statementTimeoutExecutor`, a schema
  DDL deadline that has its own setting.
- Already backend-neutral: the repo-dependency `GraphQuiescenceBudget`, which
  is read from the variable on both backends.

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
- `Neo.ClientError.Transaction.LockClientStopped`: observed live, and not a
  timeout status. Terminating a transaction for any reason stops its lock
  client (`KernelTransactionImplementation.markForTerminationIfPossible` calls
  `lockClient.stop()`), so a write waiting on another transaction's lock fails
  with this status whether a timeout, an operator's `TERMINATE TRANSACTIONS`,
  or a database shutdown terminated it. The status text says so, and the live
  test reproduces both the timeout and the operator-kill cause.

Rollback safety: `closeTransaction` checks `canCommit()` (commit requested and
no termination mark) before `commitTransaction`, so a transaction terminated
before commit is rolled back, and the terminating status reaches the client
through `failOnNonExplicitRollbackIfNeeded` or through the failed statement.
That holds by protocol for an abandoned client transaction too: the server
cannot commit an explicit transaction without the client's COMMIT, and on a
cancelled context the driver closes the connection. No live check here can
fail on a late commit, so none is claimed as proof. The residual window is a
COMMIT already in flight when the client deadline fires; that is reported as
retryable `graph_write_timeout` and relies on idempotent replay, as on
NornicDB.

Before this change, `LockClientStopped` matched the transient `"LockClient"`
substring in `classifyTransientNeo4jError`, so `RetryingExecutor` retried it
in place up to three more times before requeueing it. Each retry waits one
more full timeout while the caller still holds its lease. On ops-qa, with a
300s timeout, that is up to 20 minutes for one write.
`lockClientStoppedRequeue` now matches the exact typed status before the
substring check and requeues it at once. Every termination reason rolls the
transaction back, so a queue replay is safe in every group. Nested under a
connectivity error or driver execution limit, where the outer outcome is
unknown, it stays on the ordinary classifier path, as before. Typed-code
matching only: the
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
  only the helper renames applied, the `TransactionTimedOut` case failed. It
  also pins that a deferral records the status the backend sent; before that
  fix, a `TransactionTimedOut` deferral recorded
  `TransactionTimedOutClientConfiguration`.
- `TestRetryingExecutorRequeuesNeo4jLockClientStoppedWithoutLocalRetry`: a
  kill-shaped `LockClientStopped` (the message an operator kill produced live)
  in a single statement, an unrelated group, and a `RUNS_ON` group. Each makes
  one attempt and ends retryable with class `graph_write_timeout`. On the
  previous branch head (`7470d29337`), the single-statement and unrelated-group
  cases failed on `reducer.IsRetryable` (they would have dead-lettered), and
  the `RUNS_ON` case recorded the wrong code.
  `TestRetryingExecutorKeepsCommitAmbiguousLockClientStoppedTerminal` keeps
  the lost-during-commit shape terminal.
- `TestComposeForwardsGraphWriteBoundsToEveryGraphWriter`: run against
  origin/main's `docker-compose.neo4j.yml`, it failed with
  `compose env ESHU_CANONICAL_WRITE_TIMEOUT missing`.
- `Test{CanonicalExecutorForGraphBackend,BootstrapCanonicalExecutor,ProjectorCanonicalExecutor,SemanticEntityExecutorForGraphBackend}BoundsNeo4jWritesLikeNornicDB`:
  a write blocked past a 20ms timeout, in a non-`RUNS_ON` group, run on both
  backends. Before the parity change, every Neo4j subtest hung until the
  test's 5s guard and returned a bare `context deadline exceeded`; the NornicDB
  subtests already passed. After the change, both backends return a retryable
  `graph_write_timeout`. The companion `LeavesUnboundedNeo4jUnwrapped` tests
  pin that an unset timeout leaves the chain unchanged.
- `TestWarnUnboundedNeo4jWriteTimeoutLogsOnceWhenNeo4jHasNoTimeout`, in each
  of the four packages: exactly one WARN record for Neo4j when the variable is
  unset or invalid, and none when it is configured or on NornicDB.
- `TestLiveNeo4jIngesterCanonicalWriteTimeoutRequeues`, against
  `neo4j:2026-community` through the production Neo4j
  `canonicalExecutorForGraphBackend` chain with a 2s timeout and a held lock:
  it returned a retryable `graph_write_timeout` after 2.00s, on a freshly
  started server (the seed and cleanup writes go through an unbounded
  executor, so a cold server cannot time them out). With `boundNeo4jWrites`
  mutated to a passthrough, the same test failed on a terminal
  `LockClientStopped` after 2.67s.
- `TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite`, run against
  `neo4j:2026-community` with `ESHU_CANONICAL_WRITE_TIMEOUT=2s` through the
  production `newReducerNeo4jExecutor` seam:
  - lock wait: aborted after 2.45s with `LockClientStopped`, one attempt,
    retryable.
  - long statement: aborted after 2.29s with
    `TransactionTimedOutClientConfiguration`, one attempt, terminal.
  - operator kill, with no transaction timeout: `TERMINATE TRANSACTIONS` on
    the lock-waiting contender returned `LockClientStopped` after 0.16s, one
    attempt, retryable.
  - With the previous branch head's classifier swapped in through
    `go test -overlay`, the lock-wait and operator-kill cases failed on
    `reducer.IsRetryable(...) = false, want true`.
  - Mutation with `LockClientStopped` removed from the classifier, on an
    earlier revision: 4 attempts, 11.69s, failed `want 1`.
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
touch. The Neo4j client-deadline wrapper runs only when the timeout is set.
It adds one `context.WithTimeout` per write, the same cost NornicDB already pays
on these paths, next to a Bolt round trip. Unset Neo4j deployments get no
client-deadline wrapper. The `LockClientStopped` change does apply to them: a
write terminated during a lock wait now requeues after one attempt instead of
after three in-place retries. These figures do not estimate Neo4j throughput. On Neo4j with the
variable set, a write that previously would have hung now ends at the
configured timeout. That is the intended behavior change, and it is bounded
by the operator's own setting.

## Observability Evidence:

One new startup log event, `graph.write_timeout.unbounded` (a WARN, via
`telemetry.EventAttr`, with `graph_backend` and `env_var`), documented in the
telemetry logs reference. There is no new metric, span, or registered log key.
A Neo4j timeout surfaces through existing signals. On the client-deadline
paths, the error is a `GraphWriteTimeoutError` whose message names
`ESHU_CANONICAL_WRITE_TIMEOUT`, and the queue row retries with
`failure_class=graph_write_timeout`. On writers wrapped by `InstrumentedExecutor` (ingester, bootstrap-index,
projector), the write span records error status with the typed code.
A replay-safe `RUNS_ON` group's queue row is recorded as retrying with
`failure_class=graph_write_timeout`, which feeds the existing
`eshu_dp_reducer_retry_surge_total` and the write-timeout backpressure depth
query. Terminal rows keep the typed code in `failure_details`, and a deferred
row's error carries the code the backend actually sent. Because
`LockClientStopped` is no longer retried in place,
`eshu_dp_neo4j_deadlock_retries_total` no longer counts `reason=transient` for
it. Instead, each occurrence logs one WARN, "neo4j transaction terminated
during lock wait, requeueing without local retry", with `operation`, `code`,
and `error`, and its queue row retries with
`failure_class=graph_write_timeout`.
