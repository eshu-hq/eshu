# MCP IaC-management route selection

## Purpose

This package owns family membership and pure internal-request selection for
the seven MCP IaC-management tools: dead-IaC discovery, unmanaged-cloud-
resource discovery, the read-only management-status pair (status and
explanation), the Terraform import-plan proposer, the Terraform
config-vs-state drift finder, and the replatforming-ownership packet
builder.

## Ownership boundary

This package owns IaC-management family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration order (`find_dead_iac` and `find_unmanaged_resources`
live in `tools_codebase.go`; `get_iac_management_status`,
`explain_iac_management_status`, `propose_terraform_import_plan`,
`list_terraform_config_state_drift_findings`, and
`find_unmanaged_resource_owners` live in `tools_iac.go`), global route
fanout, the private `iacManagementRoute` adapter in `dispatch.go`, HTTP
dispatch, authorization, timeouts, response budgets, envelopes, and
telemetry. `internal/query` owns the bounded reads behind each
`/api/v0/iac/...`, `/api/v0/terraform/...`, and
`/api/v0/replatforming/ownership-packets` path.

The three sibling replatforming tools — `compose_replatforming_plan`,
`list_aws_runtime_drift_findings`, and `get_replatforming_rollups` — are not
part of this family even though `find_unmanaged_resource_owners` shares their
`/api/v0/replatforming/...` and `/api/v0/aws/...` namespace and all four used
to live together in the root `dispatch_iac.go` body-helper file. Each of
those three builds its body from its own root helper
(`replatformingPlanBody`, `awsRuntimeDriftFindingsBody`,
`replatformingRollupsBody`) that no tool in this package shares, so moving
`find_unmanaged_resource_owners` here orphans no shared helper.

## Exported surface

- `Route` selects the internal request for an IaC-management tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, while the HTTP handlers retain
the shared API request duration and error metrics (`request_metrics.go` in
`internal/query`).

## Gotchas / invariants

- The import path ends in `iacmanagement`, while the declared package is
  `iacmanagementtools`. The root imports it with an explicit alias.
- Every request is a `POST` with a JSON body and no query string, built
  fresh per call.
- `get_iac_management_status` and `explain_iac_management_status` share one
  body builder (`managementStatusBody`) because both resolve one AWS stable
  resource identity to the same fixed one-item page (`limit` 1, `offset` 0)
  and differ only in which handler renders the result — a status summary
  versus a grouped explanation. `limit` and `offset` are fixed, not caller
  arguments; a caller-supplied `limit` or `offset` is silently ignored,
  matching the pre-extraction root switch.
- The other five tools default `limit` to 100 and `offset` to 0 when the
  caller omits them, mirroring the root switch arms these route selections
  replaced. `find_unmanaged_resource_owners` never forwards `arn`; its
  ownership page is scope/account bound, not single-resource bound.
- String fields travel even when empty (an explicit blank filter), matching
  the root switch arms these route selections replaced.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"100"` — falls back to the default.
- List arguments (`repo_ids`, `families`, `finding_kinds`, `drift_kinds`)
  travel through `routecontract.Arguments.StringSlice`, the same helper the
  root `stringSlice` used: absent or malformed input serializes as `null`, a
  present list serializes as `[]any`.
- Family membership is an explicit name switch, never a prefix match.
- The **route-serves-data registry** (`route_serves_data_registry*.go` in the
  parent package) cites `internal/query` handler files by path (for example
  `go/internal/query/iac.go`, `go/internal/query/iac_management.go`,
  `go/internal/query/iac_management_surface.go`,
  `go/internal/query/iac_import_plan.go`,
  `go/internal/query/terraform_config_state_drift.go`, and
  `go/internal/query/replatforming_ownership_handler.go`), never
  `go/internal/mcp/dispatch.go` or this package, so this extraction does not
  require repointing any registry entry — confirmed by searching the
  registry files for `internal/mcp/` path references before this move (none
  found) and by running `TestRouteServesDataRegistry` after it.

No-Observability-Change: this extraction moves only pure IaC-management
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
