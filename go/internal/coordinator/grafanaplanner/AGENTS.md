# Grafana Planner Agent Guide

## Read first

1. `planner.go` — request validation, target parsing, filtering, and workflow-row construction.
2. `planner_contract_test.go` — IDs, ordering, privacy, UTC timestamps, and fairness partitions.
3. `planner_edge_test.go` — invalid inputs, all-target validation, empty selections, and trigger fallback.
4. `../grafana_service.go` and `../service_grafana_test.go` — parent scheduling and durable admission boundary.
5. `../plannercontract/README.md` — shared safe plan-key rules.

## Ownership

This package owns pure Grafana planning. It validates a five-field `PlanRequest`,
parses configured targets, selects allowed targets, and builds deterministic
workflow runs and work items. It does not resolve credentials, call Grafana,
read Postgres, admit work, claim work, retry requests, or emit telemetry.

The root coordinator owns scheduling order, clock-derived plan keys, active and
claims gates, collector-egress filtering, tenant-grant authorization, durable
open-target admission, retries, queue and lease behavior, and reconcile
telemetry. Keep those responsibilities in the parent package.

## Invariants

- Validate blank and duplicate scope IDs across every configured target before
  dropping disabled targets or applying `ScopeIDs`.
- Preserve configured target order for work items; sort only requested-scope
  metadata by scope ID.
- A valid empty selection returns a fully populated pending run and no items.
  The parent decides whether to admit it.
- Empty trigger kinds derive schedule or bootstrap. A valid explicit trigger
  takes precedence over bootstrap.
- Trim `ScopeIDs`, ignore blanks, collapse duplicates, and match exact scope IDs.
- Keep URLs, token environment names, resource limits, and stale settings out
  of `RequestedScopeSet`.
- Use target `instance_id` for the fairness conflict key when it is nonblank;
  otherwise use `scope_id`.
- Keep all timestamps in UTC and all IDs deterministic for a fixed request.

## Verification

Run focused child and parent tests first, then recursive coordinator tests and
the scoped race suite. At minimum, run the child explicitly with
`go test ./internal/coordinator/grafanaplanner ./internal/coordinator -count=1`;
mutation proofs must break the production assertion and must be reverted before
broader verification.
