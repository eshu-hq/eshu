# AGENTS.md — MCP impact-analysis route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_impact.go` and `../dispatch_impact_contract_test.go` for the
   root adapter and the production-boundary proof.
5. `../dispatch.go` for `resolveRoute`, whose default case consults the
   adapter — the same point in the chain the family's own switch answered
   from before the extraction.
6. `../ecosystem/` for eight of the nine advertised schemas, and
   `../tools_reachability.go` for `trace_exposure_path`'s. The schemas stay
   there and must keep naming the same fields these builders select.
7. `../routecontract/README.md` for the dependency-neutral request contract.
8. `go/internal/query/impact_trace_deployment.go` for the
   trace-deployment-chain handler this family's most commented default feeds:
   `normalizeTraceDeploymentChainMaxDepth` clamps `max_depth` into [0, 1000]
   rather than rejecting, and `boundedTraceEnrichmentLimit(0)` = 25 is what an
   omitted (forwarded-as-0) `max_depth` resolves to.

## Invariants

- Keep only impact-analysis family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution stay
  in the parent MCP package and `internal/query`.
- Keep the package clause as `package impacttools`; the root imports it with
  an explicit alias.
- Preserve each tool's exact method, path, and body keys. All nine requests
  are `POST` under `/api/v0/impact/` with no query string.
- Keep every selected key present even when the caller omitted it, so the
  handler sees an empty filter rather than no field at all.
- Preserve the dispatcher-side defaults the tests pin: `limit` 25 or 50 per
  route, `max_depth` 4/8/5 per route, `direct_only` true, and
  `trace_deployment_chain`'s deliberate `max_depth` 0.
- Preserve `explain_dependency_path`'s raw-map pass-through, including its
  aliasing of the caller's map. It is the family's one exception and is
  pinned by both the child and root tests.
- Return the zero request and `handled=false` for unrelated tools, including
  the ecosystem neighbours (`compare_environments`, `get_ecosystem_overview`)
  and near-miss names sharing a prefix with a family tool.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the advertised
  schema, because a mismatch changes a client-visible page size or depth.
  `trace_deployment_chain`'s `max_depth` 0 is load-bearing: forwarding a
  positive default widens the handler's resolved search limit for callers who
  changed nothing.
- Add a body key only after the handler decodes it by name; a key the handler
  does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped defaulted key (`limit`, `max_depth`, `offset`) fails silently:
  the handler substitutes its own behavior and no caller sees an error, only
  a different page. A dropped selector key silently widens or narrows
  results. The per-key assertions in both test files exist because a
  request-level comparison alone hides which key was lost.
- Wrapping `explain_dependency_path`'s body in a selecting builder would
  silently drop every argument its handler reads that the builder did not
  name.
- Claiming a name the root also answers makes resolution depend on which
  check runs first.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, `boolOr`, or `stringSlice`
  helpers; use `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/impact ./internal/mcp -count=1
go vet ./internal/mcp/impact ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
