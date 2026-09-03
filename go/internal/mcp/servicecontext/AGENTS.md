# AGENTS.md — MCP service-context route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and `../dispatch_service_selector.go`
   for the private `serviceContextRoute` adapter, consulted as a delegation
   ahead of the switch that held the four arms before the extraction.
5. `../service/context_tools.go` (for `get_service_context`,
   `get_service_story`, and `investigate_service`) and
   `../service/intelligence_tools.go` (for
   `get_service_intelligence_report`) for the four advertised schemas. They
   stay in the sibling `service` registration package and must keep naming
   the same fields this selector reads.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. The handler behind each path, for the selector-resolution and truth
   behavior this package's routes must stay compatible with. Confirmed with
   `rg --files internal/query -g '<file>'` (and `internal/serviceintelhttp`
   for the report) before being written here:

   | Tool | Handler |
   | --- | --- |
   | `get_service_context` | `internal/query/entity.go` (`getServiceContext`) |
   | `get_service_story` | `internal/query/service_story_handler.go` |
   | `investigate_service` | `internal/query/service_investigation.go` |
   | `get_service_intelligence_report` | `internal/serviceintelhttp/handler.go` |

## Invariants

- Keep only service-context family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution
  stay in the parent MCP package and `internal/query` /
  `internal/serviceintelhttp`.
- Keep the package clause as `package servicecontexttools`; the root imports
  it with an explicit alias.
- Preserve each tool's exact method, path, and query keys. All four requests
  are `GET` with a query map and no body.
- Preserve `get_service_context`'s and `get_service_story`'s selector
  validation: `Route` reports `handled=true` with a non-nil error and a zero
  `routecontract.Request` when the caller supplied no usable selector. Do not
  collapse this to the two-value `(routecontract.Request, bool)` shape the
  other route-selector siblings use -- that shape cannot carry this error, and
  silently building a request with an empty selector segment changes callers'
  observable behavior. `investigate_service` deliberately does not validate
  its selector, matching the pre-extraction root switch.
- Preserve `normalizeQualifiedIdentifier` and `canonicalWorkloadIdentifier`
  exactly: they are used only by this package now (confirmed with
  `rg -n canonicalWorkloadIdentifier\|normalizeQualifiedIdentifier` across
  `go/` before the move found no caller outside the four selector routes).
- Return the zero request, `handled=false`, and a nil error for unrelated
  tools, including `list_service_catalog_correlations`, `get_workload_context`,
  and `get_workload_story`, which stay in the root switch because each maps
  its selector without this package's normalization or validation logic.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test (`TestEveryRegisteredToolHasDispatchRoute` in `tools_test.go`),
  the shared HTTP contract, and applicable golden-corpus proof.
- Change a selector-validation error message only against this package's own
  `routes_test.go`, which asserts all three messages by exact equality, and
  `dispatch_service_story_test.go` in the parent package, which pins the
  `get_service_context` message through the full dispatch chain.
  `dispatch_service_investigation_authz_test.go` does NOT assert any message --
  it exercises a valid investigation only -- so it will not catch a reworded
  error.
- Add a tool here only after confirming the root `resolveRoute` does not
  also answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- Claiming a name the root also answers makes resolution depend on which
  check runs first -- this is why `list_service_catalog_correlations`,
  `get_workload_context`, and `get_workload_story` are deliberately excluded
  from `Route`'s switch.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, and telemetry ownership.
- Dropping the selector-validation error turns a fast, local "requires
  workload_id" failure into a live HTTP request against a malformed path
  (for example `/api/v0/services//context`); the root adapter must forward
  both `handled` and `err`, not just `handled`.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, `boolOr`, or `stringSlice`
  helpers; use `routecontract.Arguments`, whose coercions match them
  exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not pull `list_service_catalog_correlations` into this family to
  "complete" the service set; it shares no selector logic with the four
  tools here, and the parent packages' docs already document catalog routing
  as deliberately separate.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, query key, or selector-validation behavior in
  a way clients can observe.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/servicecontext ./internal/mcp -count=1
go vet ./internal/mcp/servicecontext ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
