# PagerDuty planner agent guide

## Read first

1. `planner.go` — request validation, target parsing, membership, filtering,
   and workflow-row construction.
2. `planner_contract_test.go` — exact IDs, ordering, privacy, UTC timestamps,
   trigger precedence, and fairness partitions.
3. `planner_edge_test.go` — invalid requests, all-target validation, scope
   membership, and empty selections.
4. `../pagerduty_service.go`, `../service_pagerduty_test.go`, and
   `../service_incident_freshness.go` — root scheduling, freshness handoff, and
   durable admission boundaries.
5. `../plannercontract/README.md` — shared safe plan-key rules.

## Ownership

This package owns pure PagerDuty planning and configured-target membership.
It validates the five-field `PlanRequest`, parses every configured target,
selects exact requested scopes, and builds deterministic workflow rows. It does
not resolve credentials, call PagerDuty, read Postgres, admit or claim work,
change incident-freshness trigger state, retry requests, or emit telemetry.

The root coordinator owns scheduling order, clock-derived plan keys, active and
claims gates, collector-egress filtering, tenant-grant authorization, durable
open-target admission, trigger transitions, retries, queue and lease behavior,
and reconcile telemetry. Keep those responsibilities in the parent package.

## Invariants

- Validate every configured target before applying `ScopeIDs`.
- Preserve configured target order for work items; sort only requested-scope
  metadata by scope ID.
- A valid empty selection returns a populated pending run and no items.
- Empty trigger kinds derive schedule or bootstrap. A valid explicit trigger
  takes precedence over bootstrap.
- Trim scope filters, ignore blanks, collapse duplicates, and match exact scope
  IDs.
- Keep URLs, token environment names, service allowlists, incident values, and
  limits out of `RequestedScopeSet`.
- Keep the provider in each target's fairness conflict key.
- Keep all timestamps in UTC and all IDs deterministic for a fixed request.
- Return no membership match when configuration is invalid or the requested
  scope is blank or absent; root owns the authorization decision.

## Verification

Run focused child and parent tests first, then recursive coordinator tests and
the scoped race suite. At minimum, run
`go test ./internal/coordinator/pagerdutyplanner ./internal/coordinator -count=1`.
Mutation checks must break the production assertion and must be reverted before
broader verification.
