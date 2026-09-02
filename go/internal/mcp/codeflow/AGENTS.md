# AGENTS.md — MCP code-flow route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_code_flow.go` and `../dispatch_code_flow_contract_test.go` for
   the root adapter and the production-boundary proof.
5. `../dispatch.go` for `resolveRoute`, which consults the adapter as a
   delegation ahead of its own switch — the same position in the chain the
   family's selector answered from before the extraction.
6. `../tools_code_flow.go` for the four advertised schemas. They stay at the
   parent's root and must keep naming the same six fields this builder
   selects.
7. `../routecontract/README.md` for the dependency-neutral request contract.
8. `go/internal/query/code_flow.go` for the handler behind all four paths:
   `normalize()` substitutes 25 for a nonpositive `limit`, clamps above 100,
   floors a negative `line` to 0, and rejects only a blank `repo_id`.

## Invariants

- Keep only code-flow family membership and pure argument-to-request selection
  here. Global route fanout, the private adapter, and execution stay in the
  parent MCP package and `internal/query`.
- Keep the package clause as `package codeflowtools`; the root imports it with
  an explicit alias.
- Preserve each tool's exact method, path, and body keys. All four requests
  are `POST` under `/api/v0/code/flow/` with no query string, sharing one
  six-key body.
- Keep every selected key present even when the caller omitted it, so the
  handler sees an empty filter rather than no field at all.
- Preserve the dispatcher-side defaults the tests pin: `limit` 25 and `line`
  0. The `line` 0 is load-bearing: the handler treats 0 as "no line filter"
  and reports symbol ambiguity only at 0, so a positive default would
  suppress that signal for callers who set no line.
- Return the zero request and `handled=false` for unrelated tools, including
  the code neighbours (`find_code`, `execute_language_query`,
  `find_function_call_chain`) and near-miss names sharing the `dispatch_`
  prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the advertised
  schema, because a mismatch changes a client-visible page size or, for
  `line`, whether ambiguity is reported at all.
- Add a body key only after the handler decodes it by name; a key the handler
  does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped `repo_id` fails loudly: the handler 400s every caller. A dropped
  `language`, `symbol`, `file_path`, or `line` fails silently: the page widens
  past the filter the caller named. A dropped `limit` never fails at all: the
  handler substitutes its own 25. The per-key assertions in both test files
  exist because a request-level comparison alone hides which key was lost.
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
go test ./internal/mcp/codeflow ./internal/mcp -count=1
go vet ./internal/mcp/codeflow ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
