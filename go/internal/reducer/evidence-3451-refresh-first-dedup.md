# Refresh-First Shared Projection Dedup Evidence

Issue #3451 fixed a shared-projection selector wedge where the in-memory
`LatestIntentsByRepoAndPartition` pass could undo the refresh-first ordering
from the indexed candidate reader. The live failure shape is one repo-wide
refresh intent emitted after older per-edge upsert intents: if the in-memory
dedup sorted only by `created_at`, a fixed batch window could keep selecting
upserts that defer behind a refresh intent that never reached the ready set.

No-Regression Evidence: `go test ./internal/reducer -run
'TestLatestIntentsByRepoAndPartition(KeepsRefreshFirst|Deduplicates|TripleSupersede|Empty)'
-count=1` covers the selector contract. The refresh-first regression case
models three pending inheritance-edge rows for one repository: two older
per-edge upserts plus one later repo refresh. The baseline created-at ordering
would place the refresh after both edge rows; the fixed ordering returns three
surviving rows, zero superseded rows, and `refresh-late` at index 0 before
`SelectPartitionBatch` applies its batch limit. The duplicate-row cases still
return the newest row per repository/partition key and report superseded IDs, so
the fix preserves idempotent replay behavior while changing only the primary
ordering between refresh and non-refresh rows.

Performance Evidence: the changed path is an in-memory sort over the already
loaded candidate slice. It adds one `isRepoRefreshRow` check per comparator and
does not add graph reads, graph writes, Postgres queries, queue tables, worker
counts, leases, retries, runtime knobs, or batch-size changes. Backend/version:
the path is backend-neutral Go selector code; the persisted candidate ordering
contract remains the existing Postgres indexed reader ordering
`is_refresh_intent DESC, created_at ASC, intent_id ASC` proven by #3474. Input
shape for the focused proof is three candidate rows with one refresh row and
two edge rows; terminal modeled row counts are three survivors, zero superseded
rows, and no terminal/dead-letter rows because the test exercises selection
order before readiness filtering.

No-Observability-Change: the fix changes only selector ordering after candidate
load. Operators still diagnose the path with existing shared projection and
code-call projection cycle logs (`selection_duration_seconds`,
`write_duration_seconds`, `lease_claim_duration_seconds`, blocked/deferred row
counts), shared-intent backlog/status queries, partition lease rows, reducer
execution counters, and instrumented Postgres query spans. No metric,
structured-log field, span, status row, or label was added or removed.
