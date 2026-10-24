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

Migration 231 adds `workflow_work_items_security_alert_scope_updated_idx` on
`(scope_id, updated_at DESC, work_item_id ASC) WHERE collector_kind =
'security_alert'`. The read states `collector_kind = 'security_alert'` as a
literal, so the planner can prove the predicate. Its equality column and its
exact `ORDER BY` match the key, so each target's lateral is an index-only
probe that stops at the first entry. The SQL text of the read does not change.

Migration number: open PR #9015 also ships a migration 230. This change takes
231 and leaves 230 free. Whichever of the two PRs merges second renumbers.

## Why the index is partial

The first draft was a full index on `(collector_kind, scope_id, updated_at
DESC, work_item_id ASC)`. It also changed the plan of the work-item retention
prune (`retention/work/items/sql.go`, #8829). The prune's newer-row probe
matches `collector_kind`, `scope_id`, `collector_instance_id`, and `status`
against the outer row and asks for any row newer than it. With the full index
the planner walked the target's entries from the newest down. It filtered out
every newer row whose status differed before it found one that matched. On a
target that failed for its older half and then recovered, each
`failed_terminal` probe walked past every newer `completed` row.

The partial index cannot serve that probe. `newer.collector_kind =
item.collector_kind` is a join against the outer row, not a constant, so the
planner cannot prove `collector_kind = 'security_alert'` and never considers
the index there. The prune keeps its pre-#8840 plan, the same buffers, and the
same outcome in every measured shape (below). Only `security_alert` rows pay
the extra B-tree entry on insert and update.

The container-image candidate read (`collector_kind = 'oci_registry'`) has the
same shape but is not covered by this index. It was not measured here.

## Seed

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on PostgreSQL 18.6 (the
CI-pinned `ci-postgres-bookworm` image) with every repository migration
through 229 applied. `workflow_work_items` held 561,120 rows:

- `target-big`: 120,000 `security_alert` items one minute apart.
- 49 targets with 2,880 `security_alert` items each, which is 30 days at a
  15-minute cycle.
- 100 `git` scopes with 3,000 items each (always the base status mix).
- `work_item_id` is an md5, so a retention window spans every target.
- `target-1` and `target-3` have `updated_at` ties on every other item.

Three status shapes for the `security_alert` targets:

- **base**: every 97th item `failed_terminal`, every 10th `failed_retryable`,
  the rest `completed`.
- **A**: the newest 650 items per target `completed`; older items one third
  `failed_terminal`.
- **recovered**: the older half of each target `failed_terminal`, the newer
  half `completed`.

Autovacuum was off for the table. Each shape was measured cold (no
visibility-map bits) and again after `VACUUM (ANALYZE)`, under three index
states: none (pre-#8840), the full draft index, and the partial index. Each
figure is the third execution. The prune is the shipped `pruneBatchQuery`
with a 2,000-row window from the start of the key range and a 720 h cutoff,
run under `EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back transaction.

## Security-alert read

| Shape, state | None: 1 target | None: 50 targets | Partial: 1 target | Partial: 50 targets |
| --- | --- | --- | --- | --- |
| base, cold | 35.5 ms, 6,050 buf | 62.6 ms, 12,955 buf | 0.102 ms, 13 buf | 1.04 ms, 406 buf |
| base, VACUUM | 22.3 ms, 10,256 buf | 46.1 ms, 22,827 buf | 0.095 ms, 14 buf | 0.95 ms, 366 buf |
| A, cold | 32.6 ms, 6,050 buf | 62.3 ms, 12,952 buf | 0.091 ms, 13 buf | 1.03 ms, 406 buf |
| A, VACUUM | 22.8 ms, 10,976 buf | 47.5 ms, 24,349 buf | 0.092 ms, 14 buf | 1.24 ms, 366 buf |
| recovered, cold | 32.6 ms, 6,050 buf | 63.8 ms, 13,003 buf | 0.144 ms, 13 buf | 1.13 ms, 406 buf |
| recovered, VACUUM | 24.2 ms, 10,976 buf | 47.9 ms, 24,179 buf | 0.101 ms, 14 buf | 1.04 ms, 363 buf |

"1 target" is `target-big` (120,000 items). Without the index the plan is a
bitmap heap scan (cold) or an index-only scan of the generation index (after
VACUUM), then a top-N sort. With the partial index every row is an index-only
scan of `workflow_work_items_security_alert_scope_updated_idx` with no sort.
The full draft index read the same buffers as the partial one in every row
(13 or 14 for one target, 363 to 406 for 50) at 0.090 to 0.103 ms for one
target. The earlier one-target figure of 0.032 ms and 10 buffers came from a
different seed (20 scope generations per target), so compare within this
table.
The 50 result rows had the same md5 under all three index states in every
shape, ties included.

## Retention prune

| Shape, state | None | Full draft | Partial |
| --- | --- | --- | --- |
| base, cold | 374.8 ms, 787,627 buf | 48.3 ms, 37,422 buf | 374.9 ms, 787,627 buf |
| base, VACUUM | 375.3 ms, 787,474 buf | 47.8 ms, 37,269 buf | 362.2 ms, 787,474 buf |
| A, cold | 82.1 ms, 77,665 buf | 76.5 ms, 46,399 buf | 86.1 ms, 77,665 buf |
| A, VACUUM | 82.8 ms, 77,588 buf | 76.1 ms, 46,322 buf | 79.2 ms, 77,588 buf |
| recovered, cold | 241.4 ms, 482,619 buf | 3,074.3 ms, 618,280 buf | 239.9 ms, 482,619 buf |
| recovered, VACUUM | 258.4 ms, 482,540 buf | 2,090.9 ms, 618,201 buf | 244.6 ms, 482,540 buf |

With the partial index the prune plan is the pre-#8840 plan: the newer-row
probe uses `workflow_work_items_collector_scope_generation_updated_idx`, with
the same buffer count to the buffer and 421 rows removed by filter per probe
on the recovered shape. With the full draft index the probe switched to that
index and removed 7,674 rows per probe on the recovered shape, 8x to 13x
slower. The candidate set (md5 of the candidate ids) was the same under all
three index states in every shape.

The full draft index was faster on the base shape. Keeping that gain is out of
scope here, because it costs the recovered-shape regression and a WAL tax on
every collector kind. It is a separate retention change with its own proof.

## Write cost

The write probe was `pgbench -M prepared` against the seeded table. Each
transaction inserted a `security_alert` work item, claimed it, and completed
it, as three autocommit statements. Every row matched the partial predicate,
so this is the worst case: rows of other collector kinds never enter the
index. Each run did 20,000 transactions split across the clients, after a
`CHECKPOINT`. A fixed count keeps full-page-write amortization equal across
runs. The order alternated each round (none then partial, then partial then
none).

| Clients | WAL per transaction, no index (median) | Partial index (median) | Per-round delta |
| --- | --- | --- | --- |
| 4 | 6,751 bytes | 7,426 bytes | +8.8% to +10.6%, median +9.7% |
| 8 | 7,186 bytes | 7,853 bytes | +8.4% to +9.5%, median +8.9% |

A second 4-client series gave +7.6% to +8.9% on its four complete rounds. Its
last run hit a full host disk, which other work caused.

`n_tup_hot_upd` stayed 0 in every run, with and without the index, across
about 1.7 million updates. Every update path sets `updated_at`, which
`workflow_work_items_claimable_idx` carries, so work-item updates were already
non-HOT. The new index does not take HOT updates away.

Throughput is not comparable on this host. Load average ran from 9 to 21 on
16 cores from other work. Runs in the same index state ranged from 352 to 820
TPS at 4 clients and from 697 to 1,782 at 8. The paired-round deltas ranged
from -54% to +158%. The medians were 650 against 579 TPS at 4 clients and
1,261 against 1,419 at 8. That noise is wider than any effect one more B-tree
entry could have, so this note makes no throughput claim. The review's runs of
the full draft index measured +10.5% to +25% WAL (median about 16%) at 4 to 8
clients.

Index size and build time were not re-measured for the partial index. The
host disk filled during that step.

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

`TestPruneWorkItemsLivePlanIgnoresSecurityAlertIndex`
(`go/internal/storage/postgres/retention/work/items/plan_guard_live_test.go`)
seeds 20 recovered `security_alert` targets of 2,880 items and 50 `git`
scopes of 1,000. It asserts that the prune's plan never names the new index,
that its shared buffers and the entries its newer-row probe walks stay within
1.2x of the plan with the index dropped, and that the batch outcome matches.
It then plants the full draft index in a rolled-back transaction and requires
the planner to pick it and the budget to fire. Measured: 40,000 entries walked
with the partial index and without it, 744,000 with the planted full index.
Both budgets are counts, not wall time.

The read takes only `ACCESS SHARE` locks and changes no rows, so its
concurrency is unchanged. Migration 231 is the only statement in its file, so
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
`workflow_work_items_security_alert_scope_updated_idx`. The prune keeps its
existing retention telemetry (#8829).
