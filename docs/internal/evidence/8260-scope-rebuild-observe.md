# #8260: package-consumption scope-rebuild observation (QA, read-only)

Refs #7663. Part of #7934.

Operator observation after migration 181, recorded 2026-10-19 from the
QA cluster (namespace `eshu`). Every database probe was read-only: `SELECT`,
`EXPLAIN (ANALYZE, BUFFERS)` of `SELECT` pages, and the two sidecar
deletes inside `BEGIN`/`ROLLBACK`. Reducer state came from
`/metrics` and pod logs. No dirty-mark, rebuild, or other cluster
mutation was performed: dirtying the largest scope to force a rebuild
needs a fresh arbiter-model review recorded on #8260, and no arbiter
channel was available from this session, so the live-rebuild rows stay
NOT_CHECKED below.

Baseline: [7663-backfill-page-lock](7663-backfill-page-lock.md).
Telemetry: `docs/public/reference/telemetry/package-manifest-backfill.md`.
Runbook: `docs/public/operate/park-holder-runbook.md`.

## 1. Migration 181 build on the production-size table

Ledger (`eshu_schema_migrations`, writer primary):

| path | variant | applied_at (UTC) |
| --- | --- | --- |
| `180_fact_records_terraform_state_resource_arn_idx.sql` | full | 2026-10-17 20:42:18.379089 |
| `181_fact_records_package_manifest_consumption_backfill_idx.sql` | full | 2026-10-17 20:42:26.747873 |
| `182_package_manifest_consumption_keys_partition_idx.sql` | full | 2026-10-17 20:42:27.697273 |

- Build-time bound: **<= 8.37 s** (ledger delta 180 -> 181;
  includes migrator overhead between the two receipts, so the CIC
  build itself was shorter). No schema-Job log survives for that
  window (the applying Job has since been garbage-collected), so a
  tighter per-statement duration is NOT_CHECKED.
- WAL volume: not directly measurable retroactively. Proxy:
  `pg_relation_size` of
  `fact_records_package_manifest_consumption_backfill_idx` is
  **171,958,272 bytes (164 MB)** on the current table, a lower-bound
  proxy for the CIC build WAL. Table at observation time:
  `fact_records` **13,745,504 rows / 22,913,925,120 bytes (21 GB)**.
- The statement is `CREATE INDEX CONCURRENTLY`, so fact_records
  writes were never blocked (#7004); the schema Job's xmin precheck
  and the park-holder runbook cover the old-snapshot stall mode.
- Index is serving: `pg_stat_user_indexes` shows scans against it,
  and both page probes below choose it.

## 2. Largest scope: page and start-transaction shape (no rebuild fired)

Largest manifest scope on QA at observation time:

| scope | manifest facts | sidecar keys |
| --- | --- | --- |
| `git-repository-scope:repository:r_33474efb` | 30,916 | 30,922 |

Note: this is ~31k facts, the same order as the #7663 fixture scope
(31,836), not the ~1,014,462 rows quoted in #8260. That figure came
from pre-recreate reads; the database was recreated 2026-10-16 and
the sidecar now holds 580,423 rows over its scopes
(`package_manifest_consumption_keys` 155 MB;
`package_registry_identity_keys` 2 rows / 8,192 bytes, i.e. the
largest scope has no registry keys).

Page query (`listPackageManifestConsumptionKeyScopeFactsSQL`,
prepared, LIMIT 500), `EXPLAIN (ANALYZE, BUFFERS)`:

| cursor | plan | buffers | exec time |
| --- | --- | --- | --- |
| page 1 (`''`) | Index Only Scan on the 181 index | 506 hit + 27 read | 1.12 ms |
| mid-scope (row 15,000) | Index Only Scan on the 181 index | 471 hit + 70 read | 1.51 ms |

No Sort, Seq Scan, or residual Filter. Matches the fixture
post-181 shape (507 buffers, ~2.3 ms).

Start-transaction scope-wide deletes, `BEGIN` / `EXPLAIN ANALYZE` /
`ROLLBACK` (same method as the baseline note):

| statement | plan | exec time |
| --- | --- | --- |
| `DELETE FROM package_manifest_consumption_keys WHERE scope_id` (30,922 rows) | Bitmap Heap Scan via `package_manifest_consumption_keys_partition_idx` (migration 182) | 32.99 ms |
| `DELETE FROM package_registry_identity_keys WHERE scope_id` (0 rows) | Seq Scan, tiny table | 0.07 ms |

Nothing approaches the 5 s `lock_timeout`: worst measured step is
33 ms, a ~150x margin. No new performance issue filed.

## 3. Rebuild outcomes: none observed (steady state)

- `eshu_dp_package_manifest_backfill_scope_rebuilds_total`: **no
  series exposed** — zero rebuild transactions since the reducer
  process started (~7 h uptime). `page_duration_seconds` and
  `scope_lock_wait_seconds` likewise absent.
- `eshu_dp_package_manifest_backfill_passes_total{outcome="ready"}`:
  **869**, sum 8.89 s. `cursor_updated_unixtime = 0`,
  `last_success_unixtime = 1792418122`.
- Reducer logs: passes every 30 s, `ready:true`,
  `dirty_scopes_capped:0`; zero `55P03`, `lock_timeout`,
  `scope_failed`, `left_dirty` lines in the retained window.
- `package_manifest_consumption_key_dirty_scopes`: **0 rows**.
  Marker `package_manifest_consumption_keys_v2` completed
  2026-10-16 21:29:16 UTC. `backfill_progress`: 0 rows.
- So: no `lock_timeout`, no `left_dirty`, no `cleared` from a live
  rebuild on QA. p50/max page duration and scope lock wait by
  outcome are NOT_CHECKED pending a real rebuild.

## 4. Reducer FK-insert waits: none

- `pg_stat_activity`: **0** backends with `wait_event_type='Lock'`
  at observation time.
- Park advisory lock `(5318, 7088)`: **not held** (0 granted rows),
  so passes are electing normally.
- With no rebuild in flight, no reducer foreign-key insert waited
  on a backfill scope lock during this observation. Whether an FK
  insert waits *during* a largest-scope rebuild is NOT_CHECKED
  pending the rebuild.

## Ledger rows

| # | fact | value | method |
| --- | --- | --- | --- |
| 1 | 181 applied (QA writer) | 2026-10-17 20:42:26.747873+00 | `eshu_schema_migrations` SELECT |
| 2 | 181 build-time bound | <= 8.37 s | ledger delta 180 -> 181 |
| 3 | 181 index size (WAL proxy) | 164 MB | `pg_relation_size` |
| 4 | `fact_records` size | 13,745,504 rows / 21 GB | SELECT + `pg_relation_size` |
| 5 | largest manifest scope | `r_33474efb`, 30,916 facts | grouped SELECT |
| 6 | page p1 / mid-scope exec | 1.12 ms / 1.51 ms, ~500 buffers, Index Only Scan | `EXPLAIN (ANALYZE, BUFFERS)` |
| 7 | start deletes (rolled back) | 32.99 ms / 0.07 ms | `BEGIN`/`EXPLAIN ANALYZE`/`ROLLBACK` |
| 8 | rebuild outcomes | none observed; 869 `ready` passes | `/metrics` + logs |
| 9 | FK-insert waits | 0 Lock waits; park not held | `pg_stat_activity` + `pg_locks` |
| 10 | live largest-scope rebuild | NOT_CHECKED | blocked: dirty-mark needs arbiter review on #8260 |

## What remains for #8260

Dirty-mark `r_33474efb` (one `dirty_scopes` insert, a cluster
mutation) after a fresh arbiter-model review is recorded on the
issue, let the 30 s repair loop rebuild it (~62 pages + start), then
record `page_duration_seconds` p50/max by outcome,
`scope_lock_wait_seconds`, the `scope_rebuilds_total` outcome, and
concurrent FK-insert waits. Expected wall time is seconds at the
measured per-step costs; nothing in this note suggests a 5 s
proximity.
