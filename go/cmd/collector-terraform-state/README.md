# Terraform State Collector Command

## Purpose

`cmd/collector-terraform-state` wires the Terraform-state collector runtime. It
claims one enabled collector instance, opens only configured or approved state
sources, parses redacted evidence, and commits facts through the shared
ingestion boundary.

## Ownership boundary

The command owns environment/config parsing, target-scope authorization, source
factory wiring, AWS S3/DynamoDB client setup, telemetry/bootstrap wiring,
Postgres ingestion wiring, hosted admin/metrics startup, and claim-mode service
construction.

It does not decide work-item existence, scan arbitrary buckets, read unapproved
local state, materialize graph truth, or evaluate drift. Those concerns belong
to workflow coordination, Terraform-state collector packages, reducer drift
handlers, projector/Cypher writers, and query handlers.

## Exported surface

This is a `main` package. Use `go doc -cmd ./cmd/collector-terraform-state` for
the package contract. Maintainer-facing code is in config loading, target scope
source factories, AWS clients, service construction, and command tests.

## Dependencies

- Terraform-state collector/runtime packages parse state and emit facts.
- Workflow and Postgres packages provide claim and ingestion stores.
- AWS SDK wiring is local to this command for approved S3/DynamoDB sources.
- `internal/telemetry` supplies hosted runtime and collector signals.

## Telemetry

The hosted service exposes the shared Eshu admin and metrics surface. Relevant
signals include Terraform-state claim, source, parse, redaction,
schema-resolver, and composite-capture telemetry from the runtime packages.

## Gotchas / invariants

- The command must reject ambiguous or missing collector-instance config.
- Redaction key configuration is mandatory before state evidence can be
  persisted.
- Target-scope credentials may intentionally differ from state-source
  credentials; tests cover that split.
- Approved source lists are security boundaries. Do not add fallback bucket,
  prefix, or local path discovery.
- Legacy DynamoDB lock-table env support remains compatibility glue; new config
  should use the current target-scope shape.

## Focused tests

```bash
go test ./cmd/collector-terraform-state -count=1
go test ./internal/collector/terraformstate ./internal/collector/tfstateruntime -count=1
go doc -cmd ./cmd/collector-terraform-state
```

## Related docs

- `docs/public/services/collector-terraform-state.md`
- `docs/public/services/collector-terraform-state-config.md`
- `docs/public/services/collector-terraform-state-operations.md`
- `docs/public/reference/telemetry/index.md`
