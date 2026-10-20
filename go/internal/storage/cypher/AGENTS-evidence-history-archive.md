# storage/cypher evidence and change history (archive)

Older entries relocated verbatim from
[AGENTS-evidence-history.md](AGENTS-evidence-history.md) when that file
reached the repository's 500-line cap. New dated entries go in
AGENTS-evidence-history.md, not here.

## Evidence

### 2026-06-22 — Canonical writer retryable-error propagation (#3483)

Issue #3483 reported NornicDB canonical writes dead-lettering under write
pressure ("NornicDB connection timeouts + 376 dead-letters"). Root cause:
`CanonicalNodeWriter.Write` was the only major graph writer in this package
that returned its executor errors bare (`fmt.Errorf("...: %w", err)`) without
`WrapRetryableNeo4jError`. Transient failures — driver retry-budget exhaustion
(`*neo4j.TransactionExecutionLimit`), `*neo4j.ConnectivityError`, and the
`retryableNeo4jCodes` set — therefore reached `ProjectorQueue.Fail` as
non-`projector.RetryableError` values and were classified `projection_failed`
(terminal dead-letter) at `projector_queue.go` instead of `projection_retryable`
(requeue with `retryDelay`, default 30s, bounded by `maxAttempts`, default 3).

The fix wraps all three `Write` dispatch return paths with
`WrapRetryableNeo4jError`. This is a correctness change to the error *type* on an
already-occurring failure path; it does not change Cypher shape, statement
batching, transaction scope, phase order, worker counts, leases, or the retry
classifier. The grouped-atomic conformance flag
(`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`) is intentionally left at its
documented default (`false`, phase-group path); enabling it would batch MERGE
and retract DELETE into one mixed group that `isRetryableGraphWriteGroupError`
correctly refuses to retry, which would make dead-lettering worse and would
require loosening retry classification — explicitly a non-goal of #2247.

No-Regression Evidence: backend NornicDB/Neo4j shared Cypher contract,
input shape = canonical materialization (repository + directory + file + entity
phases), conflict domain = canonical `uid` MERGE under concurrent projector
workers. `go test ./internal/storage/cypher ./internal/projector
./internal/storage/postgres -count=1` → 1771 passed (2026-06-22). New regression
`TestCanonicalNodeWriterWritePropagatesRetryable` drives all three dispatch
paths (atomic_group, phase_group, sequential) with a `*TransactionExecutionLimit`
and asserts `projector.IsRetryable(Write(...)) == true`; it fails before the fix
(bare error → dead-letter) and passes after.
`TestCanonicalNodeWriterWriteKeepsTerminalErrorsTerminal` proves a
`ConstraintValidationFailed` schema error stays non-retryable (no classifier
loosening). No graph-write throughput change: same statements, same batching,
same transactions; only the error wrapper on the failure return path changed.

Observability Evidence: the change preserves all existing canonical-write
telemetry — the `telemetry.SpanCanonicalWrite`/`SpanCanonicalRetract` spans,
  the `eshu_dp_neo4j_deadlock_retries_total` retry counter (with bounded
  `write_phase` and `reason` labels) in `RetryingExecutor`, the
  `recordAtomicWrite`/`recordAtomicFallback`
counters, and the per-phase failure `slog.WarnContext("canonical phase failed",
...)`. The operator-visible improvement is queue-side: a transient canonical
write now surfaces as queue `failure_class = projection_retryable` with a bounded
requeue rather than a terminal `projection_failed` dead letter, so dead-letter
count and `attempt_count` exposed by the projector queue now distinguish
transient backpressure from real terminal failures.
