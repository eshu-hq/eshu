# Graph-Backed Gauge Snapshots

`eshu_dp_edges_by_source_tool`, `eshu_dp_files_by_language`, and
`eshu_dp_graph_orphan_nodes` are the reducer's graph-backed observable gauges.
Their graph reads used to run inside the `/metrics` collection, so one slow or
stuck graph read held the collection lock and every scrape queued behind it
(#7062: the reducer target went `up=0` and no `eshu_dp_*` series reached the
metrics backend). They are now served from a snapshot that a background
refresher rebuilds off the scrape path:

- The refresher runs one read at a time per gauge, on its own goroutine. It
  starts under the reducer's shutdown context and the reducer waits for it to
  stop before closing the graph driver; a read cut short by shutdown is not
  counted as a failure.
- Each read has a deadline (`ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT`, default `30s`)
  and repeats `ESHU_GRAPH_GAUGE_REFRESH_INTERVAL` (default `5m`) after the
  previous one finishes.
- A gauge reports nothing until its first refresh succeeds. While refreshes
  succeed, a value is at most one interval plus one read old. A failed or
  timed-out refresh keeps serving the previous snapshot, but only until it is
  older than three intervals (`3 x ESHU_GRAPH_GAUGE_REFRESH_INTERVAL`, or
  interval plus timeout if that is larger). After that the gauge reports
  nothing, so its series goes stale exactly as it did when a failed callback
  returned an error, and alerts on the gauge value (for example
  `EshuGraphOrphanNodesHigh`) neither fire on nor hide behind a frozen number.
  `eshu_dp_gauge_snapshot_age_seconds` keeps reporting past that point.

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_gauge_snapshot_refreshes_total` | counter | Refreshes by `gauge` (`edges_by_source_tool`, `files_by_language`, `graph_orphan_nodes`) and `outcome` (`success`, `error`, `timeout`). A rising `timeout` or `error` series means the graph is slow or unreachable and the gauge is stale. |
| `eshu_dp_gauge_snapshot_refresh_duration_seconds` | histogram | Duration of each refresh by `gauge` and `outcome`; the `timeout` series sits at the configured deadline. |
| `eshu_dp_gauge_snapshot_age_seconds` | observable gauge | Age of the snapshot each `gauge` is serving right now; absent until the first successful refresh. Alert when it exceeds a few intervals. |

The `EshuGraphGaugeSnapshotStale` alert (`deploy/observability/alerts.yaml`,
`graph-orphan-prometheus-rule.yaml`) fires when a snapshot is older than 15
minutes (three default intervals) for 5 minutes, or when a gauge has had failed
refreshes and no successful one for 30 minutes. If you change
`ESHU_GRAPH_GAUGE_REFRESH_INTERVAL`, change the threshold in the rule files.

A failed refresh also logs a WARN (`gauge snapshot refresh failed; keeping the last
good snapshot until it expires`) with `gauge`, `outcome`, `elapsed_seconds`,
`timeout_seconds`, the error, `failure_class=gauge_snapshot_refresh_error` or
`gauge_snapshot_refresh_timeout`, and `snapshot_age_seconds` once a snapshot
exists.

Observability Evidence: `TestProvenanceGaugesDoNotBlockMetricsCollection`
proves a graph read that never returns cannot block meter collection;
`TestProvenanceGaugeRefreshTimeoutIsRecorded` proves the stuck read is cancelled
at the deadline and counted as a `timeout`; `TestRefreshPublishesSnapshotAndKeepsItOnFailure`
proves a failed refresh keeps the previous snapshot, counts an `error`, and logs
the WARN; `TestRefreshTimeoutRecordsTimeoutOutcome` proves no age is reported
before the first success.

No-Regression Evidence: the refresher issues the same read statements the
callbacks used to issue, once per interval per gauge instead of once per scrape
per gauge, so graph read load for these gauges drops with scrape frequency and
no query shape, index, or graph write changes. See
`docs/internal/evidence/7062-provenance-gauge-scrape.md`.
