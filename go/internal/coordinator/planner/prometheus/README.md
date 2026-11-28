# Prometheus/Mimir scheduler planner

## Purpose

`prometheus` plans one workflow work item per enabled Prometheus or
Grafana Mimir target without contacting a provider or resolving credentials.

## Ownership boundary

This package owns `PlanRequest`, target parsing, enabled and requested-scope
filtering, target validation, and deterministic workflow-row construction. The
parent coordinator retains the planner interface, scheduling order, plan-key
clock, tenant and egress filtering, durable admission, retries, and telemetry.
Methods on `coordinator.Service` remain in the parent package.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, plan key,
  optional trigger override, and optional target scope filter.
- `WorkPlanner` implements the parent's `PrometheusMimirPlanner` interface.

See `doc.go` for the godoc contract.

## Dependencies

`contract` validates plan keys. `facts`, `scope`, and `workflow` provide
stable identities and durable row contracts. This package does not import its
parent and performs no I/O.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

## Gotchas / invariants

- Run IDs remain a function of instance, trigger kind, and plan key. Work-item
  and generation IDs remain a function of instance, plan key, and target scope.
- Disabled targets are dropped before blank and duplicate enabled scopes are
  checked. Enabled targets are validated before requested-scope filtering.
- Requested-scope filters trim whitespace, ignore blanks, collapse duplicates,
  and match exact scope IDs. Work items preserve configured target order.
- Requested-scope targets sort by `scope_id` for deterministic metadata. That
  metadata omits provider URLs, token and tenant environment names, and limits.
- Blank configuration is equivalent to `{}` and plans no work.
- An explicit trigger kind overrides the instance bootstrap flag.
- An empty selected target set still returns a populated pending run. The
  parent skips durable admission when the returned item slice is empty.

No-Concurrency-Change: the planner remains a pure per-call value transform with
no shared state, goroutine, lock, claim, lease, transaction, or retry. The
parent retains the Postgres open-target admission transaction and queue
ownership. Per-target fairness keys retain the target scope as their conflict
partition.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/public/reference/source-layout.md`
