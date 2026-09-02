# AGENTS.md — MCP complexity/quality route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `codeQualityRoute`
   adapter, consulted as a delegation ahead of the switch that held the three
   arms before the extraction, and
   `../dispatch_code_quality_contract_test.go` for the production-boundary
   proof.
5. `../tools_codebase.go` and `../tools_code_quality.go` for the three
   advertised schemas. They stay at the parent's root and must keep naming
   the same fields this builder selects; `calculate_cyclomatic_complexity`
   also advertises `path` and `scope`, which neither this builder selects nor
   the handler decodes.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `go/internal/query/code.go` (`handleComplexity`),
   `go/internal/query/code_complexity_page.go`, and
   `go/internal/query/code_quality.go` for the handlers behind the two paths:
   both substitute 10 for a nonpositive `limit` and clamp above 100, the
   inspection floors a negative `offset` to 0 but rejects anything above
   10000 with HTTP 400, a blank `check` resolves to `refactoring_candidates`
   while an unsupported one rejects, and the complexity handler branches to
   list mode when `entity_id` and `function_name` are both blank.

## Invariants

- Keep only complexity/quality family membership and pure
  argument-to-request selection here. Global route fanout, the private
  adapter, and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package codequalitytools`; the root imports it
  with an explicit alias.
- Preserve each tool's exact method, path, and body keys. All three requests
  are `POST` with no query string; the two complexity tools share
  `/api/v0/code/complexity`.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all — except
  `calculate_cyclomatic_complexity`'s `entity_id`, whose absence when blank
  is itself the pinned wire shape.
- `calculate_cyclomatic_complexity` must keep sending no `limit` key, so a
  blank-selector call reaches the handler's own list default rather than a
  dispatcher-chosen one.
- Preserve the dispatcher-side defaults the tests pin: `limit` 10 on the two
  paged tools, `offset` 0, and the three `min_*` thresholds forwarded as 0 so
  the handler resolves its check-specific defaults.
- Return the zero request and `handled=false` for unrelated tools, including
  `inspect_code_inventory`, which shares the `inspect_code_` spelling but
  belongs to the structural-inventory family.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the advertised
  schema, because a mismatch changes a client-visible page size.
- Add a body key only after the handler decodes it by name; a key the handler
  does not read is inert — `path` and `scope` on the complexity schema are
  the standing example.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped `repo_id`, `language`, or `check` never fails loudly: the
  handlers accept blanks and silently widen the result or fall back to the
  default check. A dropped `limit` never fails either — the handler
  substitutes its own 10. The per-key assertions in both test files exist
  because a request-level comparison alone hides which key was lost.
- Sending `entity_id` as an empty string instead of omitting it changes the
  bytes on the wire that the root tests pin; the presence assertions exist
  for exactly that regression.
- Forwarding a positive `min_*` default would silently pin one check's
  threshold onto every other check, changing results for callers who sent
  nothing.
- Claiming a name the root also answers makes resolution depend on which
  check runs first.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` or `intOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not describe the inspection `offset` as clamped in both directions: it
  floors negatives and rejects above 10000, and the docs and tests must keep
  that asymmetry.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe, including the conditional `entity_id` and the absent `limit` on
  `calculate_cyclomatic_complexity`.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/codequality ./internal/mcp -count=1
go vet ./internal/mcp/codequality ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
