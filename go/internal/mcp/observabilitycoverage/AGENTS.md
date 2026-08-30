# AGENTS.md — MCP observability-coverage route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_observability_coverage.go` and
   `../dispatch_observability_coverage_contract_test.go` for the root adapter
   and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only observability-coverage family membership and pure
  argument-to-request selection here. Global route fanout, the private adapter,
  and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package observabilitycoveragetools`; the root
  imports it with an explicit alias.
- Preserve the tool name and its exact method, path, and query keys: `GET`
  `/api/v0/observability/coverage/correlations` with no body, carrying
  `after_correlation_id`, `coverage_signal`, `coverage_status`, `limit`,
  `observability_object_ref`, `outcome`, `provider`, `resource_class`,
  `scope_id`, `source_class`, `target_service_ref`, and `target_uid`, and
  nothing else.
- Preserve all twelve keys together. They are the widest set the repository
  router selects, and the handler reads each by name.
- Keep every key present even when the caller omitted it, so the handler sees an
  empty filter rather than no filter key at all.
- Preserve the `limit` default of 50.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `list_observability_coverage` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change the route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change the `limit` default only with the handler's own bound check, because a
  mismatch silently changes a client-visible page size.
- Change the cursor only with the query layer's keyset predicate. Paging is
  cursor-only through `after_correlation_id`; there is no `offset`.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Dropping one of the twelve keys fails in two different ways, and neither is
  the one you would guess from the key count alone. `limit` is required, so
  dropping it 400s every request with `limit is required`. A scope anchor is
  required too — one of `scope_id`, `provider`, `coverage_signal`,
  `observability_object_ref`, `target_uid`, `target_service_ref` — so dropping
  whichever one the caller supplied 400s as well. The remaining four
  (`coverage_status`, `source_class`, `resource_class`, `outcome`) fail nothing
  and silently widen the page to rows the caller excluded, which reads as
  coverage the graph does not have; dropping `after_correlation_id` silently
  breaks keyset paging so the caller re-serves page one forever. The per-key
  child and dispatch assertions exist because both shapes are invisible to a
  single request-level comparison.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend on
  which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` and `intOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/observabilitycoverage ./internal/mcp -count=1
go vet ./internal/mcp/observabilitycoverage ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
