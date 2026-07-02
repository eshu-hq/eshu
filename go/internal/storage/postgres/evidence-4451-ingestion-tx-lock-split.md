# #4451 — ingestion-TX lock split (§T8)

Evidence note for the ingestion commit shared-lock split (issue #4451).
`CommitScopeGeneration` used to hold the per-repo deferred-maintenance shared
advisory barrier from before the atomic scope/generation/fact commit through
the per-commit new-repository relationship backfill — all in ONE transaction —
so a concurrent same-repo exclusive-lock maintenance batch waited for the whole
backfill, not just the atomic commit. The fix moves the backfill into its own
short transaction that re-acquires the barrier only for its own bounded write,
AFTER the main commit released it.

## Performance

Performance Evidence: the live-Postgres integration proof
(`ingestion_tx_lock_split_integration_test.go`,
`ingestion_tx_lock_split_helpers_test.go`, gated on `ESHU_POSTGRES_DSN`) shows a
concurrent exclusive-lock maintenance transaction on the same repository waits
roughly as long as the backfill when the backfill runs inside the locked window
(the pre-fix shape, reproduced directly), but waits only for the short atomic
commit under the shipped fix — a two-sided before/after contention measurement.

## No-Regression

No-Regression Evidence: no new deadlock class — many concurrent ingestion
commits (each now two sequential transactions) interleaved with concurrent
overlapping-repo exclusive-lock maintenance batches complete within a bounded
deadline every round; and atomicity is preserved — a forced failure in the
post-commit backfill transaction never rolls back or blocks the already-durable
scope/generation/fact commit (`CommitScopeGeneration` still returns nil and the
committed generation stays durably visible). No serialization workaround. Full
`go/internal/storage/postgres` suite green, race-clean.

## Observability

Observability Evidence: new histogram
`eshu_dp_ingestion_shared_lock_hold_duration_seconds` records how long the
ingestion commit holds the shared advisory barrier for one atomic
scope/generation/fact commit, with its X1 row in
`docs/public/observability/telemetry-coverage.md`.
