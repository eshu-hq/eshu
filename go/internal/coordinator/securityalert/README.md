# Security-alert scheduler

## Purpose

`securityalert` plans one workflow work item per configured provider security
alert target. It validates target configuration and builds deterministic run,
generation, and work-item identities without opening a provider connection.

## Ownership boundary

This package owns the security-alert planning request and planner
implementation. The parent `internal/coordinator` package keeps the planner
interface, `Service` scheduling position, clock-derived plan key, tenant and
egress filtering, durable open-target admission, retry behavior, and telemetry.
The `security_alert_service.go` half stays in the parent package because its
methods use the shared `Service` type.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan key.
- `WorkPlanner` implements the parent package's `SecurityAlertPlanner`
  interface.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/plannercontract` validates the shared plan-key grammar.
- `internal/facts` builds stable generation identities.
- `internal/scope` and `internal/workflow` provide collector and durable
  workflow contracts.

The package does not import the parent coordinator package.

## Telemetry

None. Planning runs inline during the parent coordinator's reconcile pass and
inherits its reconcile metrics, workflow rows, claim status, duplicate-skip
logs, and `/api/v0/index-status` surface.

No-Observability-Change: this move adds or renames no metric, span, log field,
status field, queue, worker, lease, or runtime setting. Provider request and
fact-emission signals remain in the security-alert collector runtime.

## Gotchas / invariants

- The planner never resolves `token_env` or copies it into
  `requested_scope_set`.
- Run and work-item identities depend on the collector instance, plan key, and
  target scope. Replaying the same request returns the same identities.
- Duplicate target scope IDs are rejected before any workflow row is returned.
- The plan key is validated but not normalized.

No-Regression Evidence: the focused child test calls the production planner.
The full parent and binary package tests compile the root interface, service
call, and concrete wiring after the import move. Whole-module build and vet
verify the same `securityalert.WorkPlanner` wiring through the root
`SecurityAlertPlanner` interface.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/plannercontract/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
