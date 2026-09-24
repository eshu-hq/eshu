# terraform/drift

## Purpose

Serves `POST /api/v0/terraform/config-state-drift/findings`: reducer-owned
Terraform config-vs-state drift findings for one state-snapshot scope. See
`doc.go` for the contract.

## Ownership boundary

Owns the route, its request validation, the caller-grant filtering of the
response, and the query-layer adapter over the Postgres finding store. The
reducer that writes the findings (`internal/reducer/tfconfigstate`), the SQL
itself (`internal/storage/postgres`), and the MCP tool
`list_terraform_config_state_drift_findings` are outside this package.

## Layout

- `handler.go` — `Handler`, `FindingStore`, `FindingFilter`, `FindingRow`,
  `PostgresFindingStore`, request normalization, grant binding and the
  ambiguous-owner candidate filter.
- `config_state_evidence_access.go` — redacts evidence `scope_id` values
  outside the caller's grant.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `terraform_config_state_drift.findings.list` row.
- `handler_tracing.go` — this package's handler span seam.

## Dependencies

`internal/query/querycontract`, `internal/query/iac` (the shared
`ManagementTruncated`/`ManagementNextOffset` paging helpers),
`internal/query/tracing`, `internal/storage/postgres`, `internal/telemetry`.
`iac` does not import this package, so there is no cycle.

## Telemetry

Span `telemetry.SpanQueryTerraformConfigStateDriftFindings` per request, with
`http.route` and `eshu.capability` attributes, from the shared handler tracer.
The Postgres store is wrapped in `postgres.InstrumentedDB` with store name
`terraform_config_state_drift`. Unchanged by the #6642 move.

## Gotchas / invariants

- `internal/storage/postgres` has its own `TerraformConfigStateDriftFindingStore`,
  `TerraformConfigStateDriftFindingFilter` and `TerraformConfigStateDriftFindingRow`.
  This package's `FindingStore`, `FindingFilter` and `FindingRow` are the
  query-layer shapes; the adapter converts between them. Do not rename the
  postgres types when renaming these.
- `internal/reducer/tfconfigstate` has an unrelated
  `TerraformConfigStateDriftHandler` (the reducer write side). Root's
  `query.TerraformConfigStateDriftHandler` alias is this package's `Handler`.

## Move evidence (#6642)

`terraform_config_state_drift.go`,
`terraform_config_state_drift_evidence_access.go` and their three tests moved
here as `handler.go`, `config_state_evidence_access.go`, `handler_test.go`,
`access_test.go` and `unresolved_test.go` (`git mv`). Exported names dropped
the `TerraformConfigStateDrift` prefix the path now carries. Root forwarders
(`WriteError`, `StringVal`, `ReadJSON`, ...) became direct `querycontract`
calls, and the root `iacManagementTruncated`/`iacManagementNextOffset`
forwarders, left with no caller, were deleted in favor of direct `iac` calls.

## No-Regression Evidence

No-Regression Evidence: the request validation, limits, grant checks, store
calls and response shaping are unchanged; only package qualifiers and names
differ. The capability row keeps the same truth ceilings and required profile.

## No-Observability-Change

No-Observability-Change: same span name and tracer as the root handler, same
instrumented store name; no metric or log changes.

## Related docs

- [HTTP API: IaC, content and infra](../../../../../docs/public/reference/http-api/iac-content-infra.md)
