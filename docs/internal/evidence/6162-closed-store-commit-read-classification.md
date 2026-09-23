# Closed-store commit read is a backend restart, not a projection bug (#6162)

Change: `isNornicDBStoreClosingCommitFailure` accepts a fourth commit-side
spelling of a NornicDB restart, the Badger point-read wrap
`commit failed: DB::Get key: "<key bytes>" err: DB Closed`, so the reducer
requeues the work item after the restart instead of dead-lettering it as
`projection_bug`.

Root-Cause Evidence: Ifa run 35819550601, `fault-injection (shard 4/4)`, cell
`restart-backend-between-phase-groups`, artifact
`ifa-fault-injection-shard-4-attempt-1-failure`. `work-items.csv` carries one
`gcp_resource_materialization` row for `gcp:project:acme-demo-gcp-00` in
`dead_letter` with `failure_class=projection_bug` and the message above
(key bytes `"\x0e\x00\x00\x00\x00\x00\x00\x00\xb4"`), and the drain failed on
`residual=2 dead_letter=1`. `compose-services.log` shows NornicDB's
`✅ Server stopped gracefully` followed at 04:46:21.592Z by
`explicit transaction commit failed ... commit failed: DB::Get key` and the
Bolt `COMMIT outcome is unknown` warning, i.e. the injected restart landed on
a commit still inside validation. Source chain at NornicDB `6ac958a9`:
`BadgerTransaction.Commit` calls `validateAllConstraints` and
`validateSnapshotIsolationConflicts`, both of which read through
`badgerTx.Get`/`rtxn.Get`; Badger v4.9.6 `txn.go:470` wraps a Get failure as
`y.Wrapf(err, "DB::Get key: %q", key)` and `y/error.go:60` renders that as
`"%s err: %+v"` with `ErrDBClosed` ("DB Closed"); each validation failure
rolls the transaction back with `closeLocked(TxStatusRolledBack, true, nil)`
before `badgerTx.Commit`, so nothing durable exists for a replay to double;
`pkg/cypher/transaction.go:181` prefixes `commit failed: %w`. Eshu's guard
matched the three earlier commit-side spellings (#6142, #6278, v1.3.3
`commit failed: storage closed`) by operation prefix and fell through on this
one. The guard now requires the quoted-key opener and the exact tail; a
constraint diagnostic ends in `already exists`, so an inlined identity
carrying this text stays terminal.

No-Regression Evidence: `go test ./internal/storage/cypher ./internal/reducer
-count=1` passes (2504 tests) with the three new tests in
`retryable_error_closed_read_test.go`: the raw run-35819550601 body reaches
`reducer.IsRetryable` true with `failure_class=graph_write_timeout` through the
real `CloudResourceNodeWriter` dispatch (RED before the change: "Should be
true"), is not replayed in place by `RetryingExecutor` (one call), and five near
misses (other code, identity containing the body, no tail, no opener,
trailing text) stay terminal. No hot-path Cypher, worker, or queue behaviour
changes; the classifier runs once per failed write.

No-Observability-Change: the requeue reuses the existing
`graph_write_timeout` failure class, the `neo4j transient error` classification
label, and the reducer's structured failure log; no span, metric, log field,
or status surface is added, removed, or renamed.
