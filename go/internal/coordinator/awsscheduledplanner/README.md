# awsscheduledplanner

Plans scheduled AWS collector work from a collector instance's configuration.

## What this package owns

`WorkPlanner.PlanAWSScheduledWork` turns one `PlanRequest` — a collector
instance, an observed time, and a plan key — into a `workflow.Run` plus its
`workflow.WorkItem` set. `ScanEnabled` decodes the `scheduled_scan_enabled`
flag. `PlanRequest` carries the inputs.

## Relationship to awsfreshnessplanner

These are two halves of AWS planning, split by trigger rather than by domain:

- `awsfreshnessplanner` plans from **claimed freshness triggers**.
- this package plans from **configuration**, on a schedule.

They share one definition of target-scope parsing. This package calls
`awsfreshnessplanner.ParseTargetScopes` and `TargetAuthorized` rather than
copying them, because root's instance filter and the planner's rejection are two
halves of one authorization decision and a second copy could drift. The scoped
`AGENTS.md` in `internal/coordinator` records that as the deliberate direction
for AWS target scopes, in contrast to `ociregistry`, which does keep its own
copy of a smaller helper.

## No-Observability-Change

This package emits no signal directly. The reconcile loop that calls it stays
covered by `eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, and the scheduled-AWS
run remains visible through workflow row and claim status. Moving the planner
adds no queue, worker, lease, retry, metric, span, or log boundary.

## Gotchas / invariants

- **A standalone test binary starts with an empty AWS scanner registry.**
  `ParseTargetScopes` validates `service_kind` through
  `awsruntime.SupportsServiceKind`, which is empty until a `runtimebind` package
  `init` runs. `aws_bindings_test.go` blank-imports the bindings aggregator for
  that reason; without it these tests fail with `unsupported AWS service_kind`,
  an error that has nothing to do with the planner. The root package used to get
  the registration transitively from its other tests.
- **Service-level cases stay at root.** The `Service.Run` tests construct a
  `Service` with `fakeStore` and live in
  `../aws_scheduled_service_run_test.go`; only the planner's own unit cases moved
  here.
- Do not import the root `coordinator` package; root imports this one.
