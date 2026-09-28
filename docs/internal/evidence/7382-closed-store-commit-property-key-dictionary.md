# Closed property key dictionary at commit is a backend restart, not a projection bug (#7382)

Change: `isNornicDBStoreClosingCommitFailure` accepts a sixth commit-side
spelling of a NornicDB restart, matched by exact equality under
`Neo.ClientError.Transaction.TransactionCommitFailed`:
`commit failed: persisting property key dictionary: property key dictionary persistence requires an open database`.
With this change the reducer requeues the work item after the restart. Before
it, the item was dead-lettered as `projection_bug`.

Root-Cause Evidence: Ifa Determinism Gate runs 36210727831 (merge queue,
attempt 1), 36292315197 (`main`, attempt 1), and 36349511251 (attempt 1; attempt
2 passed) each failed `fault-injection (shard 4/4)` in cell
`restart-backend-between-phase-groups` the same way. In run 36292315197 the
cell starts its fresh stack at 03:47:13Z and drives the cassettes. The drain
then polls at `fact residual=1` from 03:47:35Z until its 4-minute budget runs
out at 03:51:19Z. It reports `dead_letter=1
[gcp_relationship_materialization/dead_letter/projection_bug=1]` with the
message `write canonical gcp relationship edges: Neo4jError:
Neo.ClientError.Transaction.TransactionCommitFailed (commit failed: persisting
property key dictionary: property key dictionary persistence requires ...`.
The other two runs log the same class and phrase. Source chain at the pinned
NornicDB build (`fix-500-e022384c`, upstream `e022384cc7fc`): the dictionary
persister in `pkg/storage/property_key_dictionary.go` (line 288) returns this
error only when its database handle is nil, which happens only after `Close`.
The Badger transaction commit in `pkg/storage/badger_transaction.go` (lines
1751-1754) wraps it as `persisting property key dictionary: %w` before the
storage write and commit, and the transaction rolls back. Nothing durable
exists for a replay to double. The Bolt executor adds the `commit failed: `
prefix. Eshu's guard matched five earlier commit-side spellings (#6142, #6278,
#6162, and v1.3.3 `commit failed: storage closed`) and fell through to terminal
on this one, so `ReducerQueue` dead-lettered the item at attempt 1.

Safety: this is durable queue replay only. The shape stays out of
`classifyTransientNeo4jError`, so the `RetryingExecutor` does not replay the
body in place after a commit failure. The GCP relationship edge writer's
statements are MATCH-MATCH-MERGE upserts that converge however many times they
run. `graph_write_timeout` is a counting failure class, so `maxAttempts` bounds
the retry. The match is exact equality of the whole body. That keeps a
constraint diagnostic terminal even when its inlined, evidence-derived identity
contains this text.

Neo4j equivalence: Neo4j already requeues the matching restart. This is
derived from neo4j-go-driver v5.28.4 source and Neo4j docs, not measured on a
live Neo4j. A database that is shutting down reports `DatabaseUnavailable`,
which the driver's `IsRetriable` retries until `TransactionExecutionLimit`. A
process that exits mid-commit surfaces as a `ConnectivityError`. Both are
classified as retryable. The gap is NornicDB-only, and a failing Neo4j test
is not expected, because Neo4j already retries this case. That is derived from
driver source, not measured. #7382's acceptance asks for graph-backend proof on
Neo4j, so the proof is a hermetic classifier test that feeds the exact NornicDB
error through the production writer.

No-Regression Evidence: the classifier adds one string equality compare on
the error path, which runs once per failed write. No Cypher shape, index,
batch size, worker, lease, or queue behaviour changes, on either backend.
`retryable_error_storage_closed_test.go` adds three tests. The first feeds the
raw body through the real `GCPCloudResourceEdgeWriter` dispatch (one edge row)
and asserts `reducer.IsRetryable` true with `failure_class=graph_write_timeout`
and the original `*Neo4jError` recoverable. It was RED before the change
("Should be true"), and it goes RED again when only the new match is removed.
The second shows `RetryingExecutor` does not replay in place (one call). The
third keeps five near misses terminal: another code, extra prefix, extra
suffix, an identity containing the body, and a plain error.

No-Observability-Change: the requeue reuses the existing retry path. The item
carries `failure_class=graph_write_timeout`, the reducer's structured failure
log records it, and a burst still trips the existing `ReducerRetrySurge` alert.
No span, metric, log field, or status surface is added, removed, or renamed.
