# Work item retention loop (#8829)

`retention.go` runs bounded `workflow_work_items` retention passes;
`retention_metrics.go` registers their instruments. The SQL and batch loop
live in `go/internal/storage/postgres/retention/work/items` (`Sweeper`), wired
in `go/cmd/workflow-coordinator/main.go` as `Service.WorkItemRetention`.

## Behavior

- On by default at a 720h age; `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0`
  turns it off, and any other age must be at least 48h. It runs only in
  active deployment mode. A zero-value `WorkItemRetentionConfig` (a `Config`
  built by hand) is off.
- `LoadConfig` reads and validates the age and
  `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` even when
  retention is off, so a bad value fails startup. Interval (5m), batches per
  pass (100) and lock timeout (2s) are fixed defaults.
- `Service.Run` fails when retention is enabled and no
  `WorkItemRetentionStore` is wired. Because `LoadConfig` enables it, every
  active-mode `Service` built from `LoadConfig` needs the store;
  `go/cmd/workflow-coordinator/main.go` always wires it.
- `startWorkItemRetention` runs passes on one goroutine, started after the
  initial run reconciliation so runs that just finished are already closed:
  a pass at start, then one per interval. At most one pass is in flight and
  ticks during a long pass are dropped. It runs apart from `Run`'s select
  loop, so a pass never delays reconcile, reap, or run reconciliation.
  `Run`'s deferred stop cancels an in-flight pass and waits for it.
- A failed pass logs at ERROR and counts `outcome="error"` but does not stop
  the coordinator; the sweeper retries the same window next tick. A pass
  stopped by shutdown logs at INFO (`workflow work item retention pass
  canceled`), counts `outcome="canceled"`, and skips the table size read.
- The table size gauges export nothing until a pass reads the catalog size,
  so dark mode, retention off, and the time before the first pass never
  read as an empty table.
- Metrics use the optional `WorkItemRetentionMetrics` interface, so existing
  `Metrics` fakes need no change.

## Telemetry

| Instrument | Kind | Labels |
| --- | --- | --- |
| `eshu_dp_workflow_coordinator_work_item_retention_passes_total` | counter | `outcome` |
| `eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds` | histogram | `outcome` |
| `eshu_dp_workflow_coordinator_work_item_retention_deleted_total` | counter | `status` |
| `eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds` | histogram | none |
| `eshu_dp_workflow_coordinator_work_items_table_bytes` | gauge | none |
| `eshu_dp_workflow_coordinator_work_items_estimated_rows` | gauge | none |

The names are literal strings, not `MetricPrefix + ...`, so the telemetry
coverage gate can see their registration. One log line per pass:
`workflow work item retention pass` (INFO),
`workflow work item retention pass failed` (ERROR), or
`workflow work item retention pass canceled` (INFO).

Operator page and runbook:
`docs/public/reference/telemetry/workflow-work-item-retention.md`. Evidence:
`docs/internal/evidence/8829-workflow-work-item-retention.md`.

## Tests

`retention_test.go` (config, Run gating, failure handling),
`retention_loop_test.go` (a blocked pass does not stall run reconciliation;
shutdown is `canceled`), and `retention_metrics_test.go` (instrument
emission, empty gauges without a reading).
