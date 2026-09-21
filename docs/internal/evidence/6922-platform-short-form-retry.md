# #6922: short-form Platform commit UNIQUE conflict — retry evidence

## Problem

The #6782 differential oracle (capture8, fix-490 image) caught a concurrent
Platform MERGE loser on one NornicDB leg:

```text
UNWIND $rows AS row MERGE (p:Platform {id: row.platform_id}) ON CREATE SET ...
Neo.ClientError.Statement.SyntaxError (commit failed: constraint violation: UNIQUE on Platform.[id])
```

1 failed execution in 29 on that leg; 0 failures in ~90 executions across the
other three legs. The retry classifier (`isNornicDBCommitTimeUniqueConflictError`,
`go/internal/storage/cypher/retrying_executor.go`) required an `already exists`
tail, so this short form went terminal instead of retrying in place.

## Fix

Accept the short form for `UNIQUE on Platform.[id]` with the commit-failure
prefix, via `isNornicDBShortFormPlatformIDUniqueConflict` in
`go/internal/storage/cypher/retryable_error.go` (beside
`isNornicDBUniqueConflictBody`). The commit prefix proves failure at commit,
not parse; the constraint address names the exact Platform id key the workload
finalizer MERGEs, so MERGE-guarded replay converges on the winning node.
Short forms naming any other label or property, UNIQUE mentions without the
commit prefix, non-MERGE Cypher, and ordinary syntax errors stay terminal
(pinned by `TestNornicDBPlatformCommitUniqueConflictRetryStaysNarrow`,
including a short-form x CREATE row so a future guard refactor cannot
silently retry non-idempotent writes). Two sibling sites deliberately still
require the `already exists` tail and classify a short form as terminal: the
`TransactionCommitFailed`/`TransactionOutdated` branch of
`isNornicDBCommitTimeUniqueConflictError` and the string-fallback
`isNornicDBCommitTimeUniqueConflict` in the same file. That narrowness is
correct for the single captured wire shape in #6922 (SyntaxError code); do
not broaden either site without fresh wire evidence.
Retry budget is unchanged (default MaxRetries 3, backoff); exhaustion still
surfaces queue-retryable via `neo4jRetryableError`. No worker, batch, query,
schema, or conflict-key change.

## Proof

- RED: the issue's exact wire shape classified terminal before the fix
  (shim `TestProve6922ShortFormIsTerminal` PASS pre-fix; the committed
  regression `TestRetryingExecutorRetriesShortFormPlatformCommitUniqueConflict`
  FAILED pre-fix with `Execute() error = ... want nil after retry`).
- GREEN post-fix: focused 5-test run PASS (short-form retry + reason,
  narrowness table, long-form retry, 2-writer convergence, live-shape pin).
- `go test ./internal/storage/cypher/ -count=1` PASS (full package, 4.6s).
- Contention proof: pre-existing
  `TestRetryingExecutorConvergesConcurrentTypedPlatformCommitUniqueConflict`
  (2 writers, 1 create, 1 conflict, both green, counter reason
  `commit_unique_conflict`); the short-form test asserts the same reason.
- Idempotency: replay target is `MERGE (p:Platform {id})` + SET, convergent
  on re-execution; group path still requires the replay-safe gate.

## Measurement basis

No-Regression Evidence: this is a correctness fix on an error-classification
branch, not a throughput change — there is no latency/throughput delta to
bench. The no-regression case is: (a) the classifier runs once per failed
write (string matching, no I/O); (b) the only new behavior is bounded in-place
retry on an error that previously went terminal, occurring at the measured
oracle rate of ~1 in 29 leg-executions on the contended path; (c) full
`storage/cypher` package green in 4.6s with `go vet` and `gofumpt` clean;
(d) backend pins unchanged (NornicDB
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468`, Neo4j
`neo4j:2026-community`). Live multi-leg oracle re-proof is CI-only per
`docs/public/reference/local-testing.md`; unit proof suffices here because the
classifier is pure string matching over the evidence-backed captured wire
shape — no backend behavior beyond that string is asserted. Next oracle run
post-merge is expected to show zero failures-kind residuals on this
fingerprint.

No-Observability-Change: reuses the existing
`eshu_dp_neo4j_deadlock_retries_total` counter with reason
`commit_unique_conflict` and `write_phase=canonical_upsert` (asserted in the
new test); no metric, span, log field, or status contract added or renamed.
Contention on the Platform conflict domain surfaces through the same signal
operators already watch.
