# #7062: graph-backed gauges no longer read the graph inside /metrics

Issue: eshu-hq/eshu#7062. Base: `6ef17e7a47`.

## Problem

`RegisterEdgesBySourceToolObservableGauge`, `RegisterFilesByLanguageObservableGauge`,
and `RegisterGraphOrphanObservableGauge` (`go/internal/telemetry/instruments.go`)
called their observers synchronously inside the OpenTelemetry observable-gauge
callback. The callback runs on the meter collection goroutine while the reader
holds its collection lock. `neo4jSessionRunner.Run` (`go/cmd/reducer/neo4j_wiring.go`)
sets no transaction timeout and the collection context carries no deadline, so
one stuck Bolt read held the lock forever. On ops-qa (reducer `sha-a49323c`,
NornicDB) about 1630 scrape handlers queued behind it, the target went `up=0`,
and no `eshu_dp_*` reducer series reached the metrics backend.

## Change

- New `go/internal/telemetry/snapshot` package: a refresher that runs one
  deadline-bounded read at a time per gauge on its own goroutine and publishes
  the result through an `atomic.Pointer`. Gauge callbacks read the last
  snapshot only; nothing is observed before the first successful refresh.
- `go/cmd/reducer/provenance_gauge_wiring.go` feeds the two provenance gauges
  and the graph-orphan gauge from that refresher, started under the reducer's
  signal (shutdown) context in `run.go`; the reducer waits for the loops before
  the graph driver closes.
- A snapshot older than three intervals (or interval plus timeout, if larger) is
  no longer observed, so a gauge whose refreshes keep failing goes stale like a
  failed callback did on main, keeping `EshuGraphOrphanNodesHigh` honest. The
  `EshuGraphGaugeSnapshotStale` alert watches `eshu_dp_gauge_snapshot_age_seconds`.
- Two env vars: `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` (5m) and
  `ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT` (30s).
- Three signals: `eshu_dp_gauge_snapshot_refreshes_total{gauge,outcome}`,
  `eshu_dp_gauge_snapshot_refresh_duration_seconds{gauge,outcome}`,
  `eshu_dp_gauge_snapshot_age_seconds{gauge}`, plus a WARN log per failed or
  timed-out refresh.

The read statements are unchanged (`ProvenanceCountStore`, `OrphanSweepStore`).
No schema, index, Cypher shape, or graph write changes.

## Regression proof (RED then GREEN)

`TestProvenanceGaugesDoNotBlockMetricsCollection` registers the gauges over a
graph reader whose `Run` blocks until its context is done (the ops-qa shape) and
calls `ManualReader.Collect(context.Background())`.

RED, on the unfixed wiring (`registerProvenanceCoverageGauges` calling the
store from the callback):

```
--- FAIL: TestProvenanceGaugesDoNotBlockMetricsCollection (2.00s)
    provenance_gauge_wiring_test.go:64: Collect() blocked on a stuck graph read; provenance gauges must not read the graph on the scrape path
FAIL	github.com/eshu-hq/eshu/go/cmd/reducer	2.767s
```

GREEN after the change: the same test passes in under a millisecond while the
refresher's read is still blocked, and
`TestProvenanceGaugeRefreshTimeoutIsRecorded` shows that read cancelled at the
deadline and counted as `outcome="timeout"` for both provenance gauges.

## No-Regression Evidence

Read load on the graph does not increase. Before, each `/metrics` scrape ran the
seven per-verb edge aggregates, the File-language group, and the orphan
anti-join counts (one per label). Now the same statements run once per
`ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` (default 5m) per gauge, on the refresher,
regardless of scrape rate. At a 15s scrape interval that is 20x fewer reads of
these statements; the per-statement cost and plan are unchanged because the
statements are byte-identical. Only one refresh per gauge is ever in flight, so
graph read concurrency from these gauges is capped at three. The scrape path
does no graph I/O and no longer takes any graph-latency-dependent time.

Trade-off, stated plainly: while refreshes succeed the three gauges lag the
graph by up to one interval plus one read. While refreshes keep failing, the
previous snapshot is served only until it is three intervals old, then the
gauge reports nothing. `eshu_dp_gauge_snapshot_age_seconds` exposes the lag and
keeps reporting after expiry.

## Observability Evidence

An operator at 3 AM sees a stale gauge through
`eshu_dp_gauge_snapshot_refreshes_total{gauge,outcome}` (rising `timeout` or
`error`), `eshu_dp_gauge_snapshot_age_seconds{gauge}` (growing), the
duration histogram (a `timeout` series pinned at the deadline), and a WARN
`gauge snapshot refresh failed; keeping the last good snapshot until it expires` carrying
`gauge`, `outcome`, `elapsed_seconds`, `timeout_seconds`, `failure_class`, and
`snapshot_age_seconds`. Tests: `TestRefreshPublishesSnapshotAndKeepsItOnFailure`,
`TestRefreshTimeoutRecordsTimeoutOutcome`, `TestShutdownStopsRefreshLoops`,
`TestOneRefreshAtATimePerSource`, `TestSourcesRefreshIndependently`,
`TestBlockedFetchNeverBlocksCounts`.

## Concurrency

One goroutine per gauge; one read in flight per gauge; the next read starts an
interval after the previous one ends, so reads never overlap or pile up.
Publication is an atomic pointer swap of an immutable snapshot (no lock is held
across a read). Shutdown: the loops start under the signal context and exit when it ends, and
`run.go` waits for them before the graph driver closes; a read cut short by
shutdown, including one that fails with a non-context "driver closed" error, is
not recorded as a failure (`TestShutdownStopsRefreshLoops`,
`TestGraphGaugeRefresherStopsWithRunContext`, `-race`). Expiry is covered by
`TestSnapshotExpiresAfterMaxAge`. A `Fetch` that ignores its context
would stall only its own loop; the bolt driver honors context cancellation, and
the symptom would be a growing snapshot age, never a blocked scrape.

## Other observable-gauge callbacks that do I/O

Checked with `rg "Observable(Gauge|Counter|UpDownCounter)|RegisterCallback"`
across `go/`. Same-shape graph reads: the three fixed here. The workflow
coordinator gauges (`internal/coordinator/metrics.go`) read atomics and do no
I/O. Postgres reads still run on the collection path and are not changed here
(same failure class if the database hangs, different backend and remedy; see
the PR description): queue depth and oldest age, shared-acceptance row count,
workflow-family queue depth, active-generation age, and poison dead-letter
counts (all in `registerReducerObservableGauges`), plus the ingester queue
observer (`cmd/ingester/main.go`). They could reuse `telemetry/snapshot` as is.
