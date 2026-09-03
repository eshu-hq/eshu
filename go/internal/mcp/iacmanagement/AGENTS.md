# AGENTS.md — MCP IaC-management route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `iacManagementRoute`
   adapter, consulted as a delegation ahead of the switch that held the
   seven arms before the extraction.
5. `../tools_codebase.go` (for `find_dead_iac` and `find_unmanaged_resources`)
   and `../tools_iac.go` (for `get_iac_management_status`,
   `explain_iac_management_status`, `propose_terraform_import_plan`,
   `list_terraform_config_state_drift_findings`, and
   `find_unmanaged_resource_owners`) for the seven advertised schemas. They
   stay at the parent's root and must keep naming the same fields this
   builder selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. The `go/internal/query` handler behind each path, for the limit/offset
   clamps and required-field checks this package's defaults must stay
   compatible with. The handler filenames do not mirror the tool names, so
   they are paired here rather than guessed. Each entry was confirmed with
   `rg --files internal/query -g '<file>'` before being written here:

   | Tool | Handler |
   | --- | --- |
   | `find_dead_iac` | `iac.go` |
   | `find_unmanaged_resources` | `iac_management.go` |
   | `get_iac_management_status` | `iac_management_surface.go` |
   | `explain_iac_management_status` | `iac_management_surface.go` |
   | `propose_terraform_import_plan` | `iac_import_plan.go` |
   | `list_terraform_config_state_drift_findings` | `terraform_config_state_drift.go` |
   | `find_unmanaged_resource_owners` | `replatforming_ownership_handler.go` |

## Invariants

- Keep only IaC-management family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution
  stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package iacmanagementtools`; the root imports
  it with an explicit alias.
- Preserve each tool's exact method, path, and body keys. All seven requests
  are `POST` with no query string.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all.
- Preserve `get_iac_management_status` and `explain_iac_management_status`'s
  fixed `limit: 1, offset: 0` — these are not caller-controlled, unlike every
  other tool in this package.
- Preserve the per-tool numeric defaults documented in `README.md`. They are
  not interchangeable across tools.
- Return the zero request and `handled=false` for unrelated tools, including
  `compose_replatforming_plan`, `list_aws_runtime_drift_findings`, and
  `get_replatforming_rollups`. Each builds its body from a helper this
  package does not share; the first and third now select from
  `internal/mcp/replatforming`, the second from the root switch.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test (`TestEveryRegisteredToolHasDispatchRoute` in `tools_test.go`),
  the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the
  advertised schema, because a mismatch changes a client-visible page size.
- Add a body key only after the handler decodes it by name; a key the
  handler does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not
  also answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped required field (for example `scope_id` on
  `list_terraform_config_state_drift_findings`) is not caught here; the
  handler decides whether an empty string 400s or widens the result. The
  per-key assertions in `routes_test.go` exist because a request-level
  comparison alone hides which key was lost.
- Claiming a name another selector also answers makes resolution depend on
  which check runs first — this is why `compose_replatforming_plan`,
  `list_aws_runtime_drift_findings`, and `get_replatforming_rollups` are
  deliberately excluded from `Route`'s switch.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, and telemetry ownership.
- `find_dead_iac` returning empty results with a Postgres-backed
  reachability store may mean the IaC reachability field is not wired in the
  binary — check `cmd/mcp-server/wiring_router.go` at
  `newMCPQueryRouterWithSemanticEmbedding` before assuming this package's
  route is wrong; the same symptom is documented in `../AGENTS.md`.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, `boolOr`, or `stringSlice`
  helpers; use `routecontract.Arguments`, whose coercions match them
  exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not pull `compose_replatforming_plan`, `list_aws_runtime_drift_findings`,
  or `get_replatforming_rollups` into this family to "complete" the IaC set;
  each owns a body shape this package does not share, and the first and third
  are owned by `internal/mcp/replatforming`.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/iacmanagement ./internal/mcp -count=1
go vet ./internal/mcp/iacmanagement ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
