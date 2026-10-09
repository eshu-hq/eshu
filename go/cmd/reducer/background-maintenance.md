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

## Id-anchor census

`startIDAnchorCensus` (issue #7212) runs only on Neo4j, because the labeled
entity-context anchor it measures is a Neo4j statement. It takes one pass at
startup, so the startup log line carries the count, then one pass per
`ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` (default one hour). Each pass is one
read-only `AllNodesScan` through the reducer's raw graph session runner (not the
differential-capture decorator, so a golden-corpus capture never records it)
under a `ESHU_ID_ANCHOR_CENSUS_TIMEOUT` deadline (default two minutes): about 1.95 s
over 1,130,424 nodes on ops-qa (image sha-57167b0, 2026-10-08). The scan is
read-only and idempotent, so replicas need no lease; each reports its own
snapshot, read at its own time, so alert on the maximum across replicas. The
result is recorded after the pass, never during a scrape.

`eshu_dp_graph_id_anchor_unreachable_nodes` is the count of nodes with an id the
anchor cannot reach. It is a snapshot of one read transaction, not a point in
time, and a failed pass leaves it at its last good value while
`eshu_dp_graph_id_anchor_census_last_success_unixtime` ages. Passes are counted
by `outcome` (`ok`, `failed`) in `eshu_dp_graph_id_anchor_census_passes_total`.
The pass log line is `id anchor census` with `snapshot=true`, the counts, and
`first_pass=true` on the startup pass; a nonzero residual logs at WARN. Shutdown
cancels the loop and waits for it before the graph driver closes.
