# Evidence: backend-restart commit failure was terminal (#6142)

## Symptom

`scripts/verify-ifa-fault-injection.sh` cell
`restart-backend-between-phase-groups` went red on PR #6142 (workflow run
32048449301 attempt 1, job 95441720646, head `49d75a4b1`): the cell's canonical
graph digest did not match the fault-free baseline, while every liveness signal
in the same cell reported healthy — 4/4 drains PASS, `residual=0`,
`dead_letter=0`, `ifa assert-edges` exact, and the restart sentinel proved
non-vacuous. A re-run of the same job at the same commit (attempt 2, job
95453325823) passed, so the failure is intermittent, not a standing red.

## Reproduction

Reproduced locally on 2026-08-17 (macOS, 12 cores, Docker 29.4.0, NornicDB
`eshu-nornicdb-pr290:3722b483c02c`, Compose project `eshu-repro-6142`, ports
15942/7901/7988) with a scratch harness that runs the gate's own
`cell_baseline` followed by repeated `restart-backend-between-phase-groups`
cells. The fault-free baseline is fully deterministic on this host: it
canonicalized to digest
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052` on every
run, byte-identical to CI's baseline digest for the failing run — 707 edges,
678 nodes, GCP relationship edges exactly 63 per `acme-demo-gcp-00..07` plus
123 for `supply-chain-demo-project`.

The first restart cell failed. It did not fail on the digest; it failed the
drain outright, and the durable state named why:

```
domain          | gcp_resource_materialization
scope_id        | gcp:project:supply-chain-demo-project
status          | dead_letter
attempt_count   | 1
failure_class   | projection_bug
failure_message | write canonical cloud resource nodes: Neo4jError:
                  Neo.ClientError.Transaction.TransactionCommitFailed
                  (commit failed: badger commit failed: Writes are blocked,
                  possibly due to DropAll or Close)

domain          | gcp_relationship_materialization
scope_id        | gcp:project:supply-chain-demo-project
status          | pending
attempt_count   | 0
```

`drains: still polling after 4m0s (fact residual=2 …)` → the gate's 4-minute
drain budget expired with `1 required-fail`.

## Root cause

A graph-backend restart interrupts a canonical write at one of four points.
Three were classified; one was not.

| point | error the backend raises | classification before this change |
|---|---|---|
| store healthy | — | — |
| **store closing, commit refused** | `Neo.ClientError.Transaction.TransactionCommitFailed` / `…badger commit failed: Writes are blocked, possibly due to DropAll or Close` | **terminal** |
| WAL closed before begin | `Neo.ClientError.Transaction.TransactionStartFailed` / `failed to write WAL tx begin: wal: closed` | retryable (`isNornicDBRestartTransactionStartFailure`) |
| process gone | `*neo4jdriver.ConnectivityError` | retryable |

`WrapRetryableNeo4jError` had no case for the commit-side point, so the error
reached the reducer as a plain `*neo4jdriver.Neo4jError`, `reducer.IsRetryable`
returned false, and the durable queue dead-lettered attempt 1 as
`projection_bug`. Because `gcp_resource_materialization` is what publishes the
`cloud_resource_uid` canonical-nodes-committed phase, the dependent
`gcp_relationship_materialization` intent then sat `pending` behind a readiness
gate that could never open, and the drain could not converge.

That is a misclassification, not a race. Once the restart lands during a
commit, the dead-letter follows every time; only *whether* the restart lands
there is timing-dependent, which is why the gate is intermittent and why a
larger, faster host does not expose it.

## Fix

`isNornicDBStoreClosingCommitFailure` in `retryable_error.go` recognizes the
commit-side twin of the existing transaction-start guard, and
`WrapRetryableNeo4jError` wraps it as `*neo4jRetryableError`
(`GraphWriteTimeoutFailureClass`), so the durable queue replays it instead of
dead-lettering. Both the code and Badger's own `ErrBlockedWrites` sentence are
required, so a genuine terminal commit failure (a UNIQUE constraint violation
under the same code) stays terminal.

The shape is deliberately NOT added to `classifyTransientNeo4jError`, so the
transaction body is never replayed in place. A commit failure leaves an outcome
this process cannot observe, and that function already excludes the driver's own
`CommitFailedDeadError` for exactly that reason. Durable queue replay needs no
such observation: it re-runs the whole handler, whose canonical writers are
MERGE-shaped upserts, and whose relationship handlers stop skipping the
prior-generation retract once `AttemptCount > 1` (`shouldSkipRetract`), so a
partially applied attempt is swept before the replay rewrites it. In-place
retry would also be useless here — the default budget is ~350 ms against a
multi-second restart.

No worker count, batch size, lease, conflict domain, Cypher shape, statement
batching, transaction scope, or phase order changes.

## Verification

Regression first, red before green
(`go/internal/storage/cypher/retrying_executor_backend_restart_commit_test.go`):
`TestBackendRestartCommitBlockedWritesRemainsQueueRetryable` failed on
`reducer.IsRetryable` before the fix and passes after;
`TestBackendRestartCommitBlockedWritesIsNotReplayedInPlace` pins the in-place
exclusion; `…ClassificationFailsClosedForNearMisses` pins the code+message
narrowness.

No-Regression Evidence: backend NornicDB `eshu-nornicdb-pr290:3722b483c02c`
over the shared Cypher/Bolt contract; input shape = the fault-injection gate's
six driven cassettes, 13 `fact_work_items`, `ESHU_REDUCER_WORKERS=4`; conflict
domain = per-scope canonical `uid` MERGE under concurrent reducer workers. This
change alters only the Go error *type* returned on an already-failing path, so
there is no new query, no extra round trip, and no measurable handler cost;
`retract_duration_seconds` / `graph_write_duration_seconds` on the touched
`gcp_relationship_materialization` completion log are unchanged.

Observability Evidence: a restart-interrupted commit is now recorded on a
`retrying` row under `failure_class=graph_write_timeout` (via
`neo4jRetryableError.FailureClass`) instead of a `dead_letter` row under
`projection_bug`, and it increments the same producer write-timeout
backpressure signal every other transient graph write does. The operator
question this answers at 3 AM — "did the graph backend bounce, or did the
projector emit a bad write?" — previously had the wrong answer recorded in
`fact_work_items.failure_class`.
