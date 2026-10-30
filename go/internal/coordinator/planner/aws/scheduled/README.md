# scheduled

Plans scheduled AWS collector work from a collector instance's configuration.

## What this package owns

`WorkPlanner.PlanAWSScheduledWork` turns one `PlanRequest` — a collector
instance, an observed time, and a plan key — into a `workflow.Run` plus its
`workflow.WorkItem` set. `ScanEnabled` decodes the `scheduled_scan_enabled`
flag. `PlanRequest` carries the inputs. The sibling `scan_interval` field is
not decoded here: root's `scheduled_work.go` reads it for every collector kind
and folds it into the plan key this package receives.

## Relationship to freshness

These are two halves of AWS planning, split by trigger rather than by domain:

- `freshness` plans from **claimed freshness triggers**.
- this package plans from **configuration**, on a schedule.

They share one definition of target-scope parsing. This package calls
`freshness.ParseTargetScopes` rather than copying it. It then applies its own
scheduled-scan rule for pairing global services with `aws-global` and regional
services with regional targets. Freshness-trigger authorization remains in
`freshness.TargetAuthorized` and the root freshness routing path.

## No-Observability-Change

This package emits no signal directly. The reconcile loop that calls it stays
covered by `eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, and the scheduled-AWS
run remains visible through workflow row and claim status. Moving the planner
adds no queue, worker, lease, retry, metric, span, or log boundary.

## Gotchas / invariants

- **No registry setup needed.** `ParseTargetScopes` validates
  `service_kind` through `awsfreshness.SupportsServiceKind` against the
  static host vocabulary. An `unsupported AWS service_kind` failure means the
  kind is genuinely absent from the vocabulary, not a missing test registration.
- **Service-level cases stay at root.** The `Service.Run` tests construct a
  `Service` with `fakeStore` and live in
  `../../../aws_scheduled_service_run_test.go`; only the planner's own unit cases moved
  here.
- Do not import the root `coordinator` package; root imports this one.
