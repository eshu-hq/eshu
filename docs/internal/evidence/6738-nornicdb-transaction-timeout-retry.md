# #6738 NornicDB transaction timeout retry boundary

## Observed failure

Ops-qa's NornicDB v1.3.3 reducer reported
`Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration` while
writing the workload materializer's three-statement atomic `RUNS_ON` group.
The deployed `ESHU_CANONICAL_WRITE_TIMEOUT` is 300 seconds. At the
2026-09-18 15:50 UTC snapshot, 17 `workload_materialization` rows were
`dead_letter` with this error; their terminal class was `projection_bug`.
The 900 retrying rows in that domain were separately classified as
`workload_materialization_resolution_not_ready`. These counts describe a
snapshot, not a completed drain.

The Eshu path is `WorkloadMaterializer.executeBatchedGroup` to
`reducerCypherExecutor.ExecuteCypherGroup` to `RetryingExecutor.ExecuteGroup`
to the Bolt managed transaction. Before this change, the exact typed error
missed the transient classifier and left the durable queue as terminal.

## Retry safety

NornicDB v1.3.3's Bolt transaction lifecycle emits this exact status only
after joining successful explicit-transaction rollback. `claimCommit` stops
the timeout timer before entering storage commit; if timeout wins first,
commit cannot start. Cleanup failure closes the connection rather than
returning this status. Focused upstream Bolt tests passed for earlier writes
being absent after timeout, cleanup failure, and commit/timeout arbitration.
The relevant source is `pkg/bolt/transaction_lifecycle.go`,
`pkg/bolt/session_transaction.go`, and
`pkg/bolt/transaction_lifecycle_compatibility_test.go` at NornicDB v1.3.3
(`a9956536`). Fetched NornicDB main `4973f2b2` has no change in these paths.
A connection loss during commit still has an unknown outcome and stays outside
this classifier.

The Eshu permission is narrower than the backend status: only
`isCanonicalRunsOnReplaySafeGroup` may turn the timeout into a durable retry.
That existing validator requires the exact workload or cross-repository
`RUNS_ON` templates, operations, and matching row chunks. The workload group
removes the same pair-bounded legacy edges, MERGEs the keyed identity, and
refreshes only the workload-owned tuple. An earlier committed Platform node
upsert also converges on replay. Other groups and single statements keep their
existing classification. The timeout is handed directly to the bounded
Postgres queue as `graph_write_timeout`; no immediate retry repeats a
five-minute transaction on the saturated backend. The queue's ordinary
attempt budget and backoff still apply.

## Local proof

The real adapter regression in `go/cmd/reducer/workload_runs_on_retry_test.go`
was RED before the classifier and GREEN afterward. It asserts the workload
materializer's atomic group makes exactly one local attempt, then returns a
retryable error classified `graph_write_timeout`. Storage/cypher tests verify
that a MERGE-shaped single statement, an unrelated group, lookalike codes and
messages, and a timeout nested under commit-ambiguous connectivity stay
terminal. The production workload adapter also preserves one local attempt and
terminal classification when the exact timeout is nested under a connectivity
failure or driver execution limit. Removing the outer-outcome guard made the
commit-loss and wrapped-limit cases fail; removing nested-timeout detection
made the transport-interruption case retry locally. Restoring both guards
passed. The existing Postgres queue
test covers bounded retry persistence for the self-classified graph timeout.

Focused commands after the code edit:

```bash
cd go && go test ./internal/storage/cypher -count=1
cd go && go test ./cmd/reducer -run '^TestWorkloadRunsOnAtomicGroup(ReplaysCommitConflicts|DefersNornicDBTransactionTimeoutToDurableRetry|DoesNotDeferTimeoutNestedInUnknownOutcome)$' -count=1
cd go && go test ./internal/storage/postgres -run '^TestReducerQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget$' -count=1
```

## Performance Evidence:

A successful `ExecuteGroup` microbenchmark on an Apple M5 Max measured five
same-machine samples. The unchanged base's median was 19.11 ns/op, 64 B/op,
one allocation. A preliminary implementation eagerly validated the group and
regressed to 28.53 ns/op median; its validation was moved behind the exact
timeout error. After the timeout-classifier edit, the revised median was 20.64 ns/op
(range 19.75–23.57). After the nested-wrapper guard edit, a fresh five-sample
run measured 18.28 ns/op median (range 17.99–18.73). Both runs held at
64 B/op and one allocation. These nanosecond samples are a local
no-material-regression check, not an ops-qa throughput estimate.

## Observability Evidence:

The queue now records the exact failed work as retrying
`failure_class=graph_write_timeout` until its bounded attempt budget is
exhausted. Existing queue status, failure counts, and retry telemetry remain
the operator signals; this change adds no raw Cypher or parameter logging.
The 36 `source_local` files-phase dead letters at the same snapshot are a
separate `GraphWriteTimeoutError` path and are not fixed by this classifier.

A 30-second NornicDB CPU profile under ops-qa load showed overlapping
cumulative stacks in `evaluateNotExistsSubquery` and
`BadgerTransaction.GetNodesByLabel`, and a block profile showed Badger read
wait. The files-phase create-missing templates contain `NOT EXISTS`, but the
profile has no query/transaction key. No exact expensive statement or
performance rewrite is claimed. Sanitized per-statement and driver-tail
elapsed times are the next measurement before a query change or replay of
those file failures.
