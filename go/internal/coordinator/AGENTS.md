# internal/coordinator Agent Rules

This package reconciles workflow control-plane rows. It plans bounded work for
explicit collector families; it MUST NOT claim work for collectors, emit facts,
open graph connections, or write graph truth.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `service.go`, `config.go`, and `metrics.go`.
3. The scheduler file for the touched family:
   `tfstate_scheduler.go`, `oci_registry_scheduler.go`,
   `package_registry_scheduler.go`, `aws_scheduled_scheduler.go`, or
   `aws_freshness_scheduler.go`.
4. `../workflow/README.md` and `../workflow/doc.go`.
5. `../telemetry/instruments.go` and `../telemetry/contract.go` before adding
   metric, span, or log names.

## Local Invariants

- Dark mode MUST remain the default. Active mode requires claims enabled and at
  least one enabled claim-capable collector instance.
- `HeartbeatInterval` MUST stay strictly lower than `ClaimLeaseTTL`.
- `Service.Run` MUST fail fast when `Store` is nil and MUST validate config
  before loops start.
- Dark mode MUST NOT reap claims or reconcile workflow runs; the nil ticker
  path protects that boundary.
- Store calls MUST stay behind the private `run*` helpers so metrics and logs
  stay consistent.
- Scheduled work admission MUST keep the open-target key:
  `(collector_kind, collector_instance_id, scope_id, acceptance_unit_id)`.
- AWS freshness handoff MUST NOT widen configured AWS access. Targets must be
  present in durable collector instance target scopes.
- Planner code MUST stay bounded and declarative. Provider I/O and claim
  ownership belong to collector runtimes.

## Change Rules

- New planner family: add the planner interface, scheduler, service hook,
  config/docs, metrics where needed, and duplicate-target tests.
- New config env var: parse, default, validate, document, and test startup
  failure for invalid values.
- Metric changes MUST use the `eshu_dp_workflow_coordinator_` prefix and the
  shared telemetry dimensions.
- Do not branch on concrete storage types. Backend behavior belongs in
  `internal/storage/postgres`.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/coordinator -count=1
go vet ./internal/coordinator
go doc ./internal/coordinator
```

Scheduling admission, claim reaping, or run reconciliation changes usually also
need `internal/workflow` and `internal/storage/postgres` tests. Docs-only edits
also need the package-doc verifier for this directory and `git diff --check`.
