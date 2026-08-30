# AGENTS.md — MCP CI/CD run-correlation route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_cicd.go` and `../dispatch_cicd_contract_test.go` for the root
   adapter and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only CI/CD run-correlation family membership and pure
  argument-to-request selection here. Global route fanout, the private adapter,
  and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package cicdtools`; the root imports it with an
  explicit alias.
- Preserve the three tool names and their exact method, path, and query keys:
  `/api/v0/ci-cd/run-correlations`, `.../run-correlations/count`, and
  `.../run-correlations/inventory`, all `GET` with no body.
- Preserve the `limit` defaults (50 on the listing, 100 on the inventory), the
  `offset` default of 0, and the `group_by` fallback to `outcome`.
- Preserve the listing route's `provider_run_id` fallback to `run_id`, and keep
  `run_id` forwarded under its own key.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `list_ci_cd_run_correlation` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only with the handler's own bound check, because a mismatch
  silently changes a client-visible page size.
- Change the `provider_run_id` fallback only with the query layer's own filter,
  because dropping it silently narrows a `run_id`-only call to every run.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
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
go test ./internal/mcp/cicd ./internal/mcp -count=1
go vet ./internal/mcp/cicd ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
