# #5989 — recover materialization after a backend restart

## Failure and ownership

The `restart-backend` cell in Ifá, the repository's fault-injection test
harness, reproduced a terminal queue transition on main after seven green
runs. The eighth run restarted the pinned NornicDB backend while the reducer held a claimed
`gcp_resource_materialization` item. The item ended as:

| Field | Observed value |
| --- | --- |
| `status` | `dead_letter` |
| `attempt_count` | `1` |
| `failure_class` | `projection_bug` |
| `lease_owner` / `claim_until` | cleared |
| failure | `Neo.ClientError.Transaction.TransactionStartFailed` — `failed to write WAL tx begin: wal: closed` |

The reducer owns the Postgres claim and heartbeats its lease while the graph
write runs. `WorkSink.Fail` clears that ownership and chooses either a scheduled
retry or a dead letter from `reducer.IsRetryable`. NornicDB emits the observed
error while beginning the transaction, before the transaction body can run, so
replaying this exact error is safe. Broader transaction-start errors and
other messages remain terminal. Commit-ambiguous connectivity errors stay out
of the immediate retry loop; their existing durable-queue behavior is
unchanged.

## Change

The Cypher error classifier now recognizes only the observed typed NornicDB
error: both its Neo4j code and message must match. The existing bounded graph
retry can then repeat the write immediately. If that budget is exhausted, the
same error keeps its typed cause and becomes a durable
`graph_write_timeout` retry instead of `projection_bug`, allowing the queue to
release the lease and reclaim the item later.

Negative tests pin the boundary: the same code with another message, the same
message under another code, and an untyped copy of the message each remain
non-retryable and call the inner executor once in both grouped and sequential
paths. The sequential path also proves a matching error retries once and that
an exhausted retry remains eligible for durable queue recovery.

## Recovery proof

The targeted probe retained the production cell's baseline digest, all three
saved replay inputs (cassettes), real reducer/projector processes, sentinel
watcher, the B-12 drain snapshot assertions, the dead-letter assertion, and the
final graph digest comparison. It only disabled unrelated fault cells in an
untracked selector overlay.

| Revision | Result | Queue evidence |
| --- | --- | --- |
| pre-fix main `39ca9f25e3` | run 8 failed after 7 green runs | one GCP materialization item dead-lettered on attempt 1 with the WAL-begin error above |
| fixed production code `32091d9393` | 10/10 consecutive runs green; every command exited `0` | every GCP materialization row ended `succeeded` on attempt 1; owner, lease, and failure fields were empty; dead-letter count was 0 |

The runtime decision logic in `32091d9393` is unchanged in the final
implementation; the later review change only strengthened tests and corrected
a Go comment. In each of the ten fixed runs the restart sentinel fired, all
terminal drain checks passed with residual count 0, and the restart digest
matched the baseline digest. The focused regression and its fail-closed
controls also pass:

```text
cd go && go test ./internal/storage/cypher -run 'Test(RetryingExecutor.*BackendRestart.*|BackendRestartTransactionStartFailure.*|RetryingExecutorExecuteGroupDoesNotRetryCommitFailedDeadConnectivityError)$' -count=1 -v; echo $?
0

cd go && go test ./internal/storage/cypher -count=1; echo $?
0
```

No golden-corpus gate (B-7), B-12, cassette, snapshot, or golden assertion
changed.

## Runtime cost and operations

No-Regression Evidence: the success and drain paths are unchanged. There is no
new Cypher, Postgres query, lock, worker, batch, timeout, or serialization. On
an error, classification adds one typed `errors.As` check plus exact string
comparisons. The post-fix B-7 golden-corpus end-to-end gate used the pinned
`eshu-nornicdb-pr290:3722b483c02c` backend and the standard 30-repository plus
35-cassette-generation local-full-stack corpus. It completed in 118 seconds;
the first drain was 64 seconds against the 75-second baseline, all three
maintenance drains ended with residual and dead-letter counts of 0, and the
gate reported 535 passes, 0 required failures, and 0 advisories. Because only
the backend-restart error path does extra work, normal drain throughput does
not regress.

Observability Evidence: the change reuses the existing structured
`neo4j transient error, retrying` log, graph-write retry counter with the
bounded `connectivity_error` reason, and durable queue
`graph_write_timeout` failure class. Operators can distinguish an immediate
retry from a later queue reclaim without a new metric or label. The successful
restart probes also expose the final queue status, attempt count, owner, lease,
failure fields, residual count, dead-letter count, and graph digest.
