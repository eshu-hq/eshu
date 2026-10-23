# Work item retention loop (#8829)

`retention.go` runs bounded `workflow_work_items` retention passes;
`retention_metrics.go` registers their instruments. The SQL and batch loop
live in `go/internal/storage/postgres/retention/work/items` (`Sweeper`), wired
in `go/cmd/workflow-coordinator/main.go` as `Service.WorkItemRetention`.

## Behavior

- Off by default: setting `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE`
  turns it on. It runs only in active deployment mode.
- `LoadConfig` reads and validates the age and
  `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` even when
  retention is off, so a bad value fails startup. Interval (5m), batches per
  pass (100) and lock timeout (2s) are fixed defaults.
- `Service.Run` fails when retention is enabled and no
  `WorkItemRetentionStore` is wired.
- The first pass runs at startup, after the initial run reconciliation, so
  runs that just finished are already closed. Later passes run on their own
  ticker.
- A failed pass logs at ERROR and counts `outcome="error"` but does not stop
  the coordinator; the sweeper retries the same window next tick.
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
`workflow work item retention pass` (INFO) or
`workflow work item retention pass failed` (ERROR).

Operator page and runbook:
`docs/public/reference/telemetry/workflow-work-item-retention.md`. Evidence:
`docs/internal/evidence/8829-workflow-work-item-retention.md`.

## Tests

`retention_test.go` (config, Run gating, failure handling) and
`retention_metrics_test.go` (instrument emission).
