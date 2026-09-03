# AGENTS.md — MCP content route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `contentRoute` adapter,
   consulted as a delegation ahead of the switch that held the five arms
   before the extraction.
5. `../tools_content.go` for the six advertised schemas registered together
   (this package's five plus `get_entity_content`). They stay at the
   parent's root and must keep naming the same fields this builder selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `../entityresolution/doc.go` and `../codeintel/routes.go` for why
   `get_entity_content` and `search_entity_content` sit in different
   families despite the shared `content`/`entity` spelling.
8. `go/internal/query/content_handler.go`, `content_reader.go`, and
   `evidence_citation.go` for the handlers behind the five paths: content
   search defaults an absent/nonpositive limit to 50 and clamps above 200,
   evidence citation defaults to 10 and caps at 50 with a 500-handle input
   cap, and `GetFileLines` validates `start_line`/`end_line` itself.

## Invariants

- Keep only content family membership and pure argument-to-request selection
  here. Global route fanout, the private adapter, and execution stay in the
  parent MCP package and `internal/query`.
- Keep the package clause as `package contenttools`; the root imports it
  with an explicit alias.
- Preserve each tool's exact method, path, and body keys. All five requests
  are `POST` with a JSON body and no query string.
- `get_file_lines` forwards the caller's arguments verbatim as the body — do
  not replace this with a freshly built map; that would silently drop any
  argument the handler decodes that this package does not enumerate.
- Preserve the dispatcher-side defaults the tests pin: `limit` 10 for
  `search_file_content`, `search_entity_content`, and
  `build_evidence_citation_packet`; `offset` 0 for the two search tools.
  These are this selector's own defaults and are not guaranteed to equal the
  handler's own default — they currently do not, for content search (10 here
  versus the handler's 50).
- `search_file_content` and `search_entity_content` must keep sharing
  `contentSearchBody`. Do not fork it — a fork can silently drift the two
  tools' repo-scope or query-fallback behavior apart.
- Return the zero request and `handled=false` for unrelated tools, including
  `get_entity_content` (owned by `entityresolution`).
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the
  advertised schema in `tools_content.go`, because a mismatch changes a
  client-visible page size or silently disagrees with the schema's stated
  default.
- Add a body key only after the handler decodes it by name; a key the
  handler does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- Replacing `get_file_lines`'s verbatim forward with a hand-picked field list
  silently drops any argument the handler reads that this package's list
  omits, without any test catching it unless the test asserts the exact body
  map.
- Claiming a name the root also answers, or that `entityresolution` answers
  for `get_entity_content`, makes resolution depend on which check runs
  first.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `stringSlice` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly. The local
  `firstString` exists only because `routecontract` has no first-element
  narrowing, and it must keep the root helper's semantics.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe, including `get_file_lines`'s verbatim-forward shape.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/content ./internal/mcp -count=1
go vet ./internal/mcp/content ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An
intentional MCP query-shape change also requires the golden-corpus gate
described by the parent package.
