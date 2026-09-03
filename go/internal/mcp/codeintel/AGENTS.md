# AGENTS.md — MCP code-intelligence route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `codeIntelRoute`
   adapter, consulted as a delegation ahead of the switch that held the eight
   arms before the extraction.
5. `../tools_codebase.go`, `../tools_code_topic.go`,
   `../tools_call_graph_metrics.go`, `../tools_structural_inventory.go`, and
   `../tools_route_to_caller.go` for the eight advertised schemas. They stay
   at the parent's root and must keep naming the same fields this builder
   selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. The `go/internal/query` handler behind each path, for the limit/offset
   clamps and required-field checks this package's defaults must stay
   compatible with. The handler filenames do not mirror the tool names, so
   they are paired here rather than guessed:

   | Tool | Handler |
   | --- | --- |
   | `find_code` | `code.go` |
   | `find_symbol` | `code_symbol.go` |
   | `inspect_code_inventory` | `code_structural_inventory.go` |
   | `inspect_call_graph_metrics` | `code_call_graph_metrics.go` |
   | `trace_route_callers` | `code_route_to_caller.go` |
   | `investigate_code_topic` | `code_topic.go` |
   | `execute_language_query` | `language_queries.go` |
   | `find_function_call_chain` | `code_call_chain.go` |

## Invariants

- Keep only code-intelligence family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution
  stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package codeinteltools`; the root imports it
  with an explicit alias.
- Preserve each tool's exact method, path, and body keys. All eight requests
  are `POST` with no query string.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all.
- Preserve the per-tool numeric defaults documented in `README.md`. They are
  not interchangeable across tools.
- Return the zero request and `handled=false` for unrelated tools, including
  `search_entity_content` and `search_file_content`, which stay in the root
  `content` child, which now owns the `contentSearchBody` helper.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test (`TestEveryRegisteredToolHasDispatchRoute` in `tools_test.go`),
  the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the
  advertised schema, because a mismatch changes a client-visible page size or
  traversal depth.
- Add a body key only after the handler decodes it by name; a key the
  handler does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not
  also answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped required field (for example `topic` on
  `investigate_code_topic`, or `symbol` on `find_symbol`) is not caught here;
  the handler decides whether an empty string 400s or widens the result. The
  per-key assertions in `routes_test.go` exist because a request-level
  comparison alone hides which key was lost.
- Claiming a name the root also answers makes resolution depend on which
  check runs first — this is why `search_entity_content` and
  `search_file_content` are deliberately excluded from `Route`'s switch.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `boolOr` helpers; use
  `routecontract.Arguments`, whose coercions match them exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not pull `search_entity_content` into this family to "complete" the
  code-search set; its body ownership belongs with `search_file_content`.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/codeintel ./internal/mcp -count=1
go vet ./internal/mcp/codeintel ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
