# Tempo scheduler planner

## Purpose

`tempoplanner` plans one workflow work item per enabled Grafana Tempo target
without contacting Tempo or resolving credentials.

## Ownership boundary

This package owns `PlanRequest`, target validation and filtering, and
deterministic workflow-row construction. The parent coordinator retains the
planner interface, scheduling order, plan-key clock, tenant and egress
filtering, durable admission, retries, and telemetry. Methods on
`coordinator.Service` remain in the parent package.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, plan key,
  optional trigger override, and optional target scope filter.
- `WorkPlanner` implements the parent's `TempoPlanner` interface.

See `doc.go` for the godoc contract.

## Dependencies

`plannercontract` validates plan keys. `facts`, `scope`, and `workflow` provide
stable identities and durable row contracts. This package does not import its
parent and performs no I/O.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

No-Observability-Change: this package move adds no metric, span, log field,
status field, queue, worker, lease, retry, or runtime setting. Coordinator
failures remain visible through `eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow row and
claim status, and the existing admission logs.

## Gotchas / invariants

- Run IDs remain a function of instance, trigger kind, and plan key. Work-item
  and generation IDs remain a function of instance, plan key, and target scope.
- Duplicate target scopes fail before disabled-target or requested-scope
  filtering.
- Work items preserve configured target order; requested-scope targets sort by
  `scope_id` for deterministic metadata.
- Requested-scope metadata omits credential environment names.
- Blank configuration is equivalent to `{}` and plans no work.
- An explicit trigger kind overrides the instance bootstrap flag.

No-Regression Evidence: direct child tests call the production planner and pin
request rejection, malformed and duplicate target handling, deterministic
identities, configured item order, sorted requested-scope order, trigger
precedence, target filtering, UTC timestamps, and credential-name omission.
Planning still parses and filters `n` configured targets in O(n), then sorts at
most `n` requested-scope rows in O(n log n). It opens no network or database
connection. Root tests pin the exact service request and durable admission
path; recursive coordinator tests, scoped race, whole-module build, and vet
cover the moved boundary. Scheduler call count and order, work-item cardinality,
fairness keys, admission lock domain, retries, and worker counts are unchanged.

No-Concurrency-Change: the planner remains a pure per-call value transform with
no shared state, goroutine, lock, claim, lease, transaction, or retry. The
parent retains the existing Postgres open-target admission transaction and all
queue ownership.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
