# AGENTS.md — MCP replatforming-planning route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_iac.go` for the private `replatformingRoute` adapter,
   consulted as a delegation ahead of the switch that held these two arms
   before the extraction, reusing the file that already owned the two body
   builders rather than creating a new one so the root non-test file count
   stays at its dirgate pin.
5. `../tools_iac.go` for the two advertised schemas
   (`compose_replatforming_plan`, `get_replatforming_rollups`). They stay at
   the parent's root and must keep naming the same fields this builder
   selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `go/internal/query/replatforming_plan_handler.go` and
   `go/internal/query/replatforming_rollups_handler.go` for the two
   handlers, and `go/internal/query/iac_management.go` /
   `go/internal/query/iac.go` for the shared `iacManagementDefaultLimit`
   (100) / `iacManagementMaxLimit` (500) clamp and the offset floor this
   package's defaults must stay compatible with. `normalizeReplatformingScope`
   in the plan handler is where a missing or unsupported `scope_kind` 400s —
   not in this package.

## Invariants

- Keep only replatforming-planning family membership and pure
  argument-to-request selection here. Global route fanout, the private
  adapter, and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package replatformingtools`; the root imports
  it with an explicit alias.
- Preserve each tool's exact method, path, and body keys. Both requests are
  `POST` with a JSON body and no query string.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all.
- Preserve the dispatcher-side defaults the tests pin: `limit` 100 and
  `offset` 0 for both tools.
- Preserve the deliberate asymmetry between the two tools: only
  `compose_replatforming_plan` forwards `arn` and `resource_id`;
  `get_replatforming_rollups` must never carry an `arn` key.
- Return the zero request and `handled=false` for unrelated tools, including
  `list_aws_runtime_drift_findings`, which stays in the parent's
  `dispatch_iac.go` even though it once shared this file's neighbourhood.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the
  advertised schema, because a mismatch changes a client-visible page size.
- Add a body key only after the handler decodes it by name; a key the
  handler does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not
  also answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped `scope_kind` on the plan route is not caught here; the handler
  400s it. A dropped `limit` never fails at all: the handler substitutes its
  own 100. The per-key assertions in `routes_test.go` exist because a
  request-level comparison alone hides which key was lost.
- Adding an `arn` key to the rollup body would silently narrow a
  scope-summary tool to one resource; the dedicated
  `TestReplatformingRollupsBodyNeverCarriesArn` test exists for exactly that
  regression.
- Claiming a name the root also answers makes resolution depend on which
  check runs first.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `stringSlice` helpers; use
  `routecontract.Arguments`, whose coercions match them exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not pull `list_aws_runtime_drift_findings` into this family; it builds
  a different body against a different path and stays in
  `dispatch_iac.go`.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe.
- Replacing the explicit name switch with derived membership.
- Adding input validation that returns an error before building a request —
  that would change `Route`'s signature to the three-value
  `(Request, bool, error)` shape `servicecontext` uses, and is a
  client-visible behavior change, not a refactor.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/replatforming ./internal/mcp -count=1
go vet ./internal/mcp/replatforming ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An
intentional MCP query-shape change also requires the golden-corpus gate
described by the parent package.
