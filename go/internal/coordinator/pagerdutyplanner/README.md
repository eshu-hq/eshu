# PagerDuty planner

## Purpose

`pagerdutyplanner` turns one validated PagerDuty collector instance into a
deterministic workflow run and one work item per selected target. It also checks
whether an incident-freshness scope belongs to a configured target; the root
coordinator combines that membership result with its authorization policy.

## Ownership boundary

The package owns request validation, PagerDuty target decoding, duplicate and
field validation, requested-scope filtering, freshness-scope membership,
trigger selection, requested-scope serialization, stable IDs, and fairness
metadata. It performs no network, credential, or database work.

The root `internal/coordinator` package keeps service scheduling order, the
clock and plan-key cadence, active and claims gates, collector-egress filtering,
tenant-grant authorization, durable open-target admission, incident-freshness
trigger transitions, retries, queue and lease behavior, and telemetry.
`internal/collector/pagerduty` owns provider calls and fact emission after a
worker claims the planned item.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, plan key,
  optional trigger override, and optional scope filter.
- `WorkPlanner` implements the root coordinator's `PagerDutyPlanner` interface.
- `HasConfiguredScope` validates collector configuration and checks exact
  target membership for the root freshness-authorization policy.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/plannercontract` validates safe plan keys.
- `internal/facts` builds stable generation IDs.
- `internal/scope` supplies the PagerDuty collector kind.
- `internal/workflow` supplies collector configuration validation and durable
  run and work-item contracts.

## Telemetry

This pure planner emits no telemetry. The root coordinator records
`eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`; workflow rows,
claim status, incident-freshness failure classes, and admission logs expose
durable progress.

No-Observability-Change: this package move adds no metric, span, log field,
status value, queue, worker, lease, retry, or runtime setting. The same root
signals continue to cover scheduling and handoff failures.

## Gotchas / invariants

- Validate every configured target before requested-scope filtering. Invalid or
  duplicate unselected targets still reject the request.
- Work items keep configured target order. `RequestedScopeSet` sorts targets by
  scope ID for stable JSON.
- A valid request with no selected target still returns a populated pending run
  and no items. The root skips durable admission for that empty item slice.
- An explicit valid trigger overrides bootstrap. An empty trigger derives
  bootstrap or schedule from the collector instance.
- Scope filters trim whitespace, ignore blanks, collapse duplicates, and match
  configured scope IDs exactly.
- Requested-scope JSON contains target identity only. It omits PagerDuty URLs,
  token environment names, service allowlists, incident values, and limits.
- Fairness keys keep the provider as the per-instance conflict partition.
- Run, generation, and work-item IDs are deterministic for a fixed request.
- `HasConfiguredScope` does not match blank or unconfigured scopes or malformed
  configuration.

No-Regression Evidence: direct child tests call the production planner and pin
request errors, all-target validation, scheduled/bootstrap/webhook trigger
precedence, configured item order, exact IDs, provider fairness partitions,
UTC timestamps, sorted privacy-safe metadata, membership, and empty
selections. Root tests pin all five request fields, scheduled and freshness
admission behavior, and distinct planning versus workflow-handoff failures.
Planning still scans `n` configured targets in O(n) and sorts at most `n`
requested-scope rows in O(n log n). It opens no network or database connection.

No-Concurrency-Change: the planner remains a pure per-call value transform with
no shared state, goroutine, lock, claim, lease, transaction, or retry. The root
retains the Postgres admission transaction and incident-freshness trigger state
transitions. Provider-based fairness keys retain their prior conflict domain.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
- `docs/public/observability/telemetry-coverage.md`
