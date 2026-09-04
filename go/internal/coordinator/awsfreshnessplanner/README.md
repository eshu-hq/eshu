# AWS freshness scheduler planner

## Purpose

`awsfreshnessplanner` turns a batch of claimed AWS freshness triggers into one
workflow run and one claimable work item per unique
`(account_id, region, service_kind)` target, without contacting AWS or
resolving credentials.

## Ownership boundary

This package owns `PlanRequest`, the AWS collector instance's `target_scopes`
parsing and normalization, the target authorization decision, trigger
coalescing, and deterministic workflow-row construction. The parent
coordinator retains the `AWSFreshnessPlanner` interface
(`service_aws_freshness.go`), the trigger claim/lease/reap loop, scheduling
order, the plan-key clock, durable open-target admission, retries, and
telemetry. Methods on `coordinator.Service` remain in the parent package.

`service_aws_freshness.go` keeps the `AWSFreshnessPlanner` interface and names
`awsfreshnessplanner.PlanRequest` in its method signature; `WorkPlanner`
satisfies it structurally, with no explicit declaration and no import of the
parent package.

## Exported surface

- `PlanRequest` carries the collector instance, the claimed trigger batch, the
  observation time, and the plan key.
- `WorkPlanner` implements the parent's `AWSFreshnessPlanner` interface via
  `PlanAWSFreshnessWork`.
- `TargetScope` is one normalized `target_scopes[]` entry: the authorized
  account, regions, and service kinds.
- `ParseTargetScopes` decodes and normalizes the whole `target_scopes` array
  from an AWS collector instance configuration document.
- `TargetAuthorized` reports whether a freshness target falls inside at least
  one normalized scope.

The last three are exported for the same reason `gcpplanner` exports
`EnabledScopes` and `ValidateClaimSchedulerConfiguration`: root code needs the
child's configuration parsing and must not reach into private types. See
`doc.go` for the godoc contract.

## Why the shared parsing is exported rather than copied

Two root files depend on this parsing for reasons of their own:

- `service_aws_freshness.go` calls `ParseTargetScopes` and `TargetAuthorized`
  in `findAWSFreshnessInstance` to route a claimed trigger to the collector
  instance allowed to collect it.
- `awsscheduledplanner/planner.go` — a different, extracted family — calls
  `ParseTargetScopes` and plans from `[]TargetScope`.

`ociregistry` (#6491) kept its own copy of the five-line pure `firstNonBlank`
helper rather than exporting it, and that remains right for a helper of that
size. This block is not that: it is roughly eighty lines of configuration
decoding plus the authorization predicate, and root's routing filter and the
planner's rejection are two halves of one decision. Two copies that drift
would silently disagree about which AWS targets a collector instance may
collect. So this follows the `gcpplanner` precedent instead — one definition,
exported — and keeps `firstNonBlank`-style duplication for genuinely tiny pure
helpers.

`awsscheduledplanner`'s own exported `ScanEnabled` still decodes the
configuration document itself, because it reads the sibling
`scheduled_scan_enabled` flag with different blank-input and validation
semantics. Its inline struct keeps the `target_scopes` field so a
type-malformed `target_scopes` array fails there exactly as it did before this
move.

## Dependencies

`internal/collector/awscloud/freshness` supplies `StoredTrigger`, `Target`,
and the freshness/scope/acceptance identities. `internal/collector/awscloud/awsruntime`
supplies `SupportsServiceKind`, which rejects a configured `service_kind` no
registered scanner implements. `plannercontract` validates plan keys. `facts`,
`scope`, and `workflow` provide stable identities and durable row contracts.
This package does not import its parent and performs no I/O.

## Telemetry

None. Planning inherits the parent reconcile metrics, the AWS freshness
handoff counters, and workflow/claim status.

No-Observability-Change: this package move adds no metric, span, log field,
status field, queue, worker, lease, retry, or runtime setting. Coordinator
failures remain visible through
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, the AWS freshness
handoff event counters recorded in `service_aws_freshness.go`, and workflow
row and claim status.

## Gotchas / invariants

- Run IDs are `aws:<instance_id>:webhook:<plan_key>`. Work-item and generation
  IDs are a function of instance, plan key, and the target's `scope_id`;
  `GenerationID` and `SourceRunID` are deliberately equal. `FairnessKey` is
  `aws:<instance_id>:<account_id>` — per account, not per target, so one
  account cannot starve another.
- Targets are coalesced by `FreshnessKey()` and emitted sorted by that key, so
  two triggers on the same tuple produce one work item and the plan is
  deterministic.
- One unauthorized target fails the whole plan; it is not silently dropped.
  Root filters first in `findAWSFreshnessInstance`, so an unauthorized target
  reaching the planner means the two sides disagreed — keep them on the one
  shared `TargetAuthorized`.
- `allowed_regions` and `allowed_services` reject empty and `*` wildcard
  entries, and every `service_kind` must satisfy
  `awsruntime.SupportsServiceKind`. That registry is empty until a
  `runtimebind` package's `init` runs, so this package's tests blank-import
  the bindings aggregator (`aws_bindings_test.go`) exactly as the parent's
  test binary does. Without it, valid service kinds would be rejected and the
  tests would fail for the wrong reason.
- Requested-scope metadata carries only `collector_instance_id` and, per
  target, `account_id`, `region`, `service_kind`, and `scope_id` — never
  credentials or the raw configuration.
- The `scheduled_scan_enabled` flag lives in the same configuration document
  but belongs to the scheduled AWS family at root; this package does not read
  it.

No-Regression Evidence: `go test ./internal/coordinator/awsfreshnessplanner ./internal/coordinator -count=1`
proves request validation, target-scope parsing and normalization, duplicate
trigger coalescing, unauthorized-target rejection on its specific error text,
scope/acceptance/generation identities, and the root claim-lease, handoff, and
failure paths through `fakeAWSFreshnessPlanner`. This is a same-behavior file
move: no lease, conflict-key, retry, batching, or ordering change.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
