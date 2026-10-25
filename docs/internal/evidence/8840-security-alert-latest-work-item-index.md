# Security-Alert Latest Work Item Index (#8840)

The security-alert empty-answer collection-state read
(`query.PostgresSecurityAlertCollectionStateStore.SecurityAlertTargetStates`,
#8803) finds the newest `security_alert` work item for each target scope. It
orders by `updated_at DESC, work_item_id ASC` and takes one row. Before this
change the only index with the `(collector_kind, scope_id)` prefix was
`workflow_work_items_collector_scope_generation_updated_idx`. That index puts
`generation_id`, a hash, before `updated_at`, so the read scanned every work
item the target ever had and then top-1 sorted them. The cost grew with the
target's history until the 1 s probe deadline cut it off and the answer failed
closed as `collector_state_unavailable`.

Migration 233 adds `workflow_work_items_collector_scope_updated_idx` on
`(collector_kind, scope_id, updated_at DESC, work_item_id ASC)`. The read's
equality prefix and its exact `ORDER BY` match the key, so each target's
lateral is an index-only probe that stops at the first entry. The SQL text of
the read does not change.

The index is not partial. The container-image candidate explanation read
(`collector_kind = 'oci_registry' AND scope_id = $1 ORDER BY updated_at DESC
LIMIT 1`) has the same shape and can use it too. That read was not measured
here.

Work-item retention (#8829) now deletes completed rows older than 720 h by
default. That bounds a 15-minute target to about 2,880 rows. It does not bound
an operator who sets a longer age or turns retention off (`0`). This index
makes the read's cost independent of history length either way.

## Seed

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on PostgreSQL 18.6 (the
CI-pinned `ci-postgres-bookworm` image) with every repository migration through
229 applied. `workflow_work_items` held 561,120 rows:

- `target-big`: 120,000 `security_alert` items one minute apart (10%
  `failed_retryable`, a few `failed_terminal`).
- 49 targets with 2,880 `security_alert` items each, which is 30 days at a
  15-minute cycle.
- 100 `git` scopes with 3,000 items each.

Each target had 20 scope generations. Autovacuum was off for the table so the
cold state (no visibility-map bits) could be measured on purpose. Each plan
below is the third execution, so the buffers are warm in shared memory. The
query is the shipped `securityAlertTargetStatesQuery` with the scope array
inlined.

## Before and after

| Read | Before: plan | Before: time, buffers | After: time, buffers |
| --- | --- | --- | --- |
| `target-big`, before VACUUM | Bitmap heap scan of 120,000 rows, top-N sort | 26.582 ms, 5,038 | 0.032 ms, 10 |
| `target-big`, after VACUUM | Index-only scan of 120,000 rows, top-N sort | 16.658 ms, 2,170 | 0.033 ms, 10 |
| `target-7` (2,880 items), before VACUUM | Bitmap heap scan, top-N sort | 0.587 ms, 131 | 0.021 ms, 10 |
| 50 targets, before VACUUM | Same per target | 94.091 ms, 11,306 | 0.662 ms, 415 |
| 50 targets, after VACUUM | Same per target | 37.009 ms, 5,108 | 0.715 ms, 366 |

After the index, every target reads 4 index buffers plus a 4-buffer primary
key fetch. The 120,000-item target and the 2,880-item target cost the same 10
buffers. Before VACUUM the probe makes one heap fetch per target to check
visibility. After VACUUM it makes none.

The after-VACUUM "before" rows came from a twin database where autovacuum had
already run. The other rows came from one database: the before-VACUUM run, then
`CREATE INDEX CONCURRENTLY`, then the after runs, then `VACUUM (ANALYZE)`.

## Write cost

The concurrent build on 561,120 rows took 0.49 s. The index is 36 MB
(37,601,280 bytes). The existing generation index on the same rows is 73 MB.
The table heap is 98 MB, and its indexes together were 291 MB with the new one.

Each new work item adds one B-tree entry. Each update adds one more as well.
Work-item updates are already non-HOT, because
`workflow_work_items_claimable_idx` carries `status`, `visible_at`, and
`updated_at`, which every claim and completion changes. The write probe was
`pgbench`, one client, 4,000 transactions per run. Each transaction inserted a
work item, then updated it to `claimed`, then to `completed`. The probe ran
five rounds, alternating with and without the index, each after a `CHECKPOINT`:

| Round | WAL bytes per transaction, with | Without | Delta |
| --- | --- | --- | --- |
| 1 | 7,208 | 6,539 | +10.2% |
| 2 | 7,330 | 6,776 | +8.2% |
| 3 | 7,834 | 7,120 | +10.0% |
| 4 | 8,160 | 7,327 | +11.4% |
| 5 | 8,285 | 7,700 | +7.6% |

The median WAL cost is +10.0% per insert, claim, and complete cycle. Latency
was the same within noise on the two rounds where the host was quiet
(0.776 ms against 0.779 ms, and 0.763 ms against 0.766 ms). The other three
rounds ran under load from other work on the shared host, and their latency
(0.8 ms to 19.9 ms) is not a usable comparison.

## Correctness

`TestSecurityAlertCollectionStatePlanLive`
(`go/internal/query/security_alert_collection_state_postgres_live_test.go`)
seeds 50 targets with 2,000 items each, one target with 20,000, `updated_at`
ties on every odd target, and 100,000 `git` items. It asserts three things:

- The plan is an index-only scan of the new index with no
  `Sort Key: l.updated_at`, both before and after VACUUM.
- The rows equal the rows of the pre-#8840 plan. The test forces that plan by
  dropping the index inside a transaction that it rolls back, and it checks
  that the dropped index is absent from the plan.
- The tie, long-history, and missing-target rows have the expected values.

With migration 233 removed from the embed, the test fails at the plan
assertion.

The read takes only `ACCESS SHARE` locks and changes no rows, so its
concurrency is unchanged. Migration 233 is the only statement in its file, so
the migration coordinator builds it in autocommit mode without the bootstrap
`lock_timeout` (#7004). Claims, heartbeats, and completions keep writing during
the build.

## Observability

No-Observability-Change: the read already runs as stage
`security_alert_collection_state`. Its `supply chain query stage completed`
log carries `duration_seconds`, and a probe past the 1 s deadline logs
`error_class=deadline_exceeded` and counts in
`eshu_dp_query_security_alert_empty_answers_total`. See
[8803-security-alert-empty-answer-truth.md](8803-security-alert-empty-answer-truth.md#observability).
An operator who sees `duration_seconds` climb on this stage can check that the
index exists and is valid with `pg_index.indisvalid` for
`workflow_work_items_collector_scope_updated_idx`.
