# #6738 admin replay and container image identity fences

Root-Cause Evidence: After the `sha-f89b050` image reached ops-qa on 2026-09-18,
145 `container_image_identity` reducer items remained in `dead_letter` with the
old FIPS MD5 failure. A forced, exact-item `POST /api/v0/admin/replay` failed
with SQLSTATE 23514 on `fact_work_items_container_image_identity_v2_status_check`.
The admin store changed `status` to `pending` without changing the required v2
and v3 authorized statuses. Postgres rejected the UPDATE; no item was replayed.
The failed request left its idempotency key `in_progress`, so recovery must
inspect the item and use a fresh key after the fixed image is deployed.

The admin store now moves the authorized statuses in the same row UPDATE as the
status transition. Replay sets them to `pending`; dead-letter sets them
to `dead_letter`. Each remains empty when its cutover guard is not required.
The claim epoch, last attempt time, existing attempt-count floor, and selector
and limit semantics are preserved. The shared admin UPDATE also rechecks terminal
status on its target row after it obtains the row lock. A concurrent worker can
claim a row after the selection CTE reads it; the target recheck prevents admin
replay or dead-letter from clearing that newer lease. The reducer's existing
replay path uses the same status authorization rule.

No-Regression Evidence: The local real-Postgres regression uses isolated
temporary tables with both cutover checks. It covers v2-only, v3-only, both,
and neither guard; already-succeeded work; repeat replay; preserved attempt and
claim-epoch history; replay events; and guarded dead-letter. Before the status
authorization fix, `TestAdminStoreIdentityFenceMutationsLive` failed with
SQLSTATE 23514; after it, the test passed. A second local PostgreSQL contention
proof held a competing claim row lock until each admin mutation waited for it.
Before the target-row predicate, replay stole the newer claim (one item
replayed); after it, replay and dead-letter each changed zero items and the
claim owner remained intact.

For the operator-only UPDATE cost, eight paired `EXPLAIN (ANALYZE, BUFFERS)`
passes on the same local PostgreSQL 18 Alpine container reset 145 unguarded
terminal rows before each old/new statement. Median execution time was
0.171 ms old and 0.1745 ms new. This measures the added assignments and target
predicate on a small, warm, unguarded fixture; it is not an ops-qa throughput or
full-corpus result. The selector order, row cap, worker settings, and queue
claim path are unchanged.

Focused proof commands, after the final code edit:

```bash
ESHU_ADMIN_REPLAY_FENCE_LIVE=1 ESHU_POSTGRES_DSN=<isolated-loopback-Postgres> \
  go test ./internal/query/admin -run '^TestAdminStore(IdentityFenceMutations|TerminalMutationsRecheckStatus)Live$' -count=1
go test ./internal/query/admin/... -count=1
```

Observability Evidence: Existing `admin_replay_requests` and `fact_replay_events`
record replay attempts and outcomes. `/admin/status` exposes queue and
per-domain dead-letter counts; reducer queue telemetry exposes claim, retry,
and completion. The ops-qa replay and terminal-state readback remain a separate
deployed verification step after this change reaches the cluster.
