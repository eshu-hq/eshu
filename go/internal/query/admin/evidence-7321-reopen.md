# Admin reopen performance and observability evidence (#7321)

`POST /api/v0/admin/reopen` (go/internal/query/admin/reopen.go,
go/internal/query/admin/store/reopen.go) replaces the hand-written
`#7285` ops-qa repair SQL with an audited, idempotency-keyed admin
surface. There is no prior endpoint to regress against: the baseline is
the repair run by hand, unbounded and unaudited.

No-Regression Evidence (#7321): EXPLAIN ANALYZE on seeded rows proves
every reopen statement is index-backed and bounded. Backend: local
`postgres:18` Docker container, full bootstrap schema (181 migrations).
Input shape: one scope, one active generation, 300 `succeeded` reducer
rows (`workload_materialization`), request limit 1000. Measured with
`EXPLAIN (ANALYZE, BUFFERS)` through a throwaway test (deleted after
the run) driving the production query constants:

- `reopenReducerCandidatesQuery`: Index Scan on
  `fact_work_items_reducer_conflict_claim_idx`, 300 rows, 0.224 ms
  execution, 9 shared hits.
- `reopenReducerLockQuery` (`FOR UPDATE SKIP LOCKED` over the candidate
  ids): 300 rows locked, 0.220 ms execution. The planner chose a
  sequential scan only because the scratch table holds 301 rows; the
  predicate is the `work_item_id` primary key with an `ORDER BY` that
  matches the deadlock-avoiding lock order of the other claim paths.
- `reopenReducerWorkQuery` (`UPDATE ... RETURNING`): Index Scan on
  `fact_work_items_reducer_source_claim_idx` with the domain, status,
  and id-list index condition, 300 rows reopened, 5.155 ms execution.

Terminal row counts are bounded by construction: the candidate select
carries `LIMIT` (`DefaultReopenLimit` 1000), the lock and the update
touch only the locked ids, and the update re-checks the full selection
predicate so EvalPlanQual drops rows a concurrent writer moved. The
transaction sets `LOCAL lock_timeout = '5s'` and every lock is
`SKIP LOCKED`, so a reopen never waits on a worker-held row; contended
rows are reported as skipped, not reopened. The intent path
(`reopenIntentCandidatesQuery`, `DISTINCT ON` one row per acceptance
unit) reuses the same select-lock-update shape. The live proof
`TestAdminHandler_ReopenLive` reopens the fixture rows and claims them
through the production reducer claim path, and a repeated call with the
same idempotency key returns the recorded outcome without touching a
row. This endpoint adds no worker, queue, or background load: it runs
only when an operator calls it.

Observability Evidence (#7321): every reopen decision appends one
`admin_recovery_action` governance audit event with no raw identifiers:
`reopen_accepted` on success, `reopen_refused_*` (unknown domain, missing
reason, missing idempotency key, unauthorized, unknown scope, no active
generation) on refusal, `reopen_idempotent_replay` on a duplicate key,
and `reopen_idempotency_key_reused` on a key reused with different
selectors. The 200 response carries the terminal counts
(`reopened_count`, `work_item_ids`, `duplicate`). No new Prometheus
instruments: the audit ledger is the 3 AM signal, the same convention
as the neighbouring `/api/v0/admin/replay` route (replay/admin/recovery
flows have no dedicated trace namespace; see
docs/public/reference/telemetry/traces.md). The endpoint reference is
docs/public/reference/http-api/status-admin-reopen.md.
