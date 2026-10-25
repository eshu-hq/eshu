# Reducer background maintenance

The resolution engine starts four loops beside its hosted queue drain in
`go/cmd/reducer/run.go`. They use the signal context and stop before the
Postgres pool closes.

## Search-document catch-up

`startSearchDocumentSweeper` (design 430) runs curated search-document
projection catch-up at a 30-second default interval. Its enqueue is
idempotent by domain, scope, and generation, so replicas need no lease.

## Config-state-drift catch-up

`startConfigStateDriftCatchUpSweeper` (issue #5593) scans active
`state_snapshot:*` scopes and re-enqueues `config_state_drift` reducer
intents every five minutes by default. It closes the gap when the ingester's
post-commit Ack trigger fails to enqueue. Enqueue is idempotent by domain,
scope, and generation. The sweeper does not inspect per-item outcomes or
re-run a handler that already has a `fact_work_items` row. It is separate
from the ledger-backed redrive removed during #5593 review; see
`ConfigStateDriftRuntimeTrigger` and `go/cmd/reducer/AGENTS.md` before
changing either path.

## Package-consumption sidecar repair

`startPackageManifestConsumptionKeyBackfill` runs the #7088 repair loop
immediately, then every second while the initial build remains incomplete.
After the sidecars are ready, contended, or a pass fails, it retries every
30 seconds. The reducer owns this derived read model
because registry and manifest facts can arrive from different collectors.
Each pass competes for a distinct session-level PostgreSQL advisory lock on
a dedicated connection. A contending replica skips that pass. The owner
rebuilds in bounded, scope-first transactions; retries are periodic after a
failure, and a stopped owner releases its session lock. Shutdown cancels the
loop and waits for it before closing the database.

The storage backfill establishes a versioned marker after both manifest and
registry identity keys are complete. Old-writer changes mark affected scopes
dirty; the API readiness reader reports the package-consumption family as
unavailable until the scope repair commits and the dirty fence clears. Once
the marker exists, periodic passes repair only dirty scopes. Operators can use
`eshu_dp_package_manifest_backfill_passes_total` by `outcome` to separate contention, failure, incomplete work, and readiness;
`eshu_dp_package_manifest_backfill_duration_seconds` measures each election
and pass. `eshu_dp_package_manifest_backfill_last_success_unixtime` gives
pass age. `eshu_dp_package_manifest_backfill_cursor_updated_unixtime` shows
initial-scope progress, and the capped dirty-scope gauge shows 0..26, where 26
means at least two 25-scope passes remain. Both gauges are sampled after each
successful elected pass, not during Prometheus scrapes. Structured reducer pass logs and storage key=value per-scope process logs
retain detailed context; the reader's unavailable
reason distinguishes missing initial backfill from a fresh old-writer mutation.

## Secret-lines side table repair

`startSecretLinesRepair` (`run.go`, #8833) runs `lines.Repairer` from
`internal/storage/postgres/secret/lines`. Every
`ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL` (default 30 s) it reads
`content_file_secret_lines_state`. A `ready` state is a no-op. Any other state
(`not_built` after a migration that changes the derivation function, or a
`failed` or abandoned bootstrap finalize) gets two non-waiting tries on a
dedicated connection: `pg_try_advisory_lock(5318,1)`, the run lock, then
`pg_try_advisory_lock(5318,2)`, which is free only while no bootstrap-index
connection is alive (every one holds it shared). When either is taken the poll
skips (`skipped_locked`, `reason=locked|deferred_writers`). When both are
granted, the Repairer re-reads the state and runs `lines.Finalize` for the
current epoch, then releases both. The locks come before the claim because the
claim is not single-flight; without them two replicas collide on the side
table's primary key, and without `(5318,2)` a bootstrap-index whose pinned
lock connection died would be published over while its other connections still
write.

The lock connection comes from its own pool (`openSecretLinesLockPool`: the
runtime opener, then `SetMaxOpenConns(1)`, `SetMaxIdleConns(0)`), never the
shared one. A rebuild pins a connection for its whole length while `Finalize`
needs the shared pool, so with `ESHU_POSTGRES_MAX_OPEN_CONNS=1` taking the lock
from the shared pool left `Finalize` waiting for the connection the lock held.
The cost is one extra connection: the reducer opens up
to `ESHU_POSTGRES_MAX_OPEN_CONNS` + 1. The lock pool opens it for the startup
ping, for each poll that finds the side table not ready (a try that may skip),
and for the whole rebuild; a ready side table opens none. A pinned session sends `SELECT 1` every
30 s so an `idle_session_timeout` does not end it. A failed poll backs off,
doubling up to 1 h; a poll cut short by shutdown counts `outcome=cancelled` and
logs a Warn. The loop is on by default; `ESHU_SECRET_LINES_REPAIR_ENABLED=false`
is for diagnostics only and leaves reads on the legacy scan. Shutdown cancels it,
waits for it, and then closes the lock pool, all before the database closes.
Signals: `eshu_dp_secret_lines_repair_runs_total{outcome}` and the
`secret_lines.repair_*` logs.

## Id-anchor census

`startIDAnchorCensus` (issue #7212) runs only on Neo4j, because the labeled
entity-context anchor it measures is a Neo4j statement. It takes one pass at
startup, so the startup log line carries the count, then one pass per
`ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` (default one hour). Each pass is one
read-only `AllNodesScan` through the reducer's raw graph session runner (not the
differential-capture decorator, so a golden-corpus capture never records it)
under a `ESHU_ID_ANCHOR_CENSUS_TIMEOUT` deadline (default two minutes): about 1.95 s
over 1,130,424 nodes on the QA graph (image sha-57167b0, 2026-10-08). The scan is
read-only and idempotent, so replicas need no lease; each reports its own
snapshot, read at its own time, so alert on the maximum across replicas. The
result is recorded after the pass, never during a scrape.

`eshu_dp_graph_id_anchor_unreachable_nodes` is the count of nodes with an id the
anchor cannot reach. It is a snapshot of one read transaction, not a point in
time, and a failed pass leaves it at its last good value while
`eshu_dp_graph_id_anchor_census_last_success_unixtime` ages. The same pass
records `eshu_dp_graph_id_anchor_id_bearing_nodes`, the count of nodes that carry
an id, because an empty graph also reads zero unreachable nodes: the healthy
reading is unreachable `0` with id-bearing above `0`, both from `/metrics`. Passes are counted
by `outcome` (`ok`, `failed`) in `eshu_dp_graph_id_anchor_census_passes_total`.
The pass log line is `id anchor census` with `snapshot=true`, the counts, and
`first_pass=true` on the startup pass; a nonzero residual logs at WARN. Shutdown
cancels the loop and waits for it before the graph driver closes.
