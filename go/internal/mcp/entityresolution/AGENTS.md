# AGENTS.md — MCP entity-resolution route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `entityResolutionRoute`
   adapter, consulted as a delegation ahead of the switch that held the three
   arms before the extraction, and
   `../dispatch_entity_resolution_contract_test.go` for the
   production-boundary proof.
5. `../tools_context.go` and `../tools_content.go` for the three advertised
   schemas. They stay at the parent's root and must keep naming the same
   fields this builder selects; `get_entity_context` also advertises
   `environment`, which this builder forwards but the handler never decodes.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `go/internal/query/entity.go` (`resolveEntity`, `getEntityContext`),
   `go/internal/query/entity_resolve_page.go`
   (`normalizeResolveEntityLimit`), and `go/internal/query/content_handler.go`
   (`readEntity`) for the handlers behind the three paths: the resolve
   handler substitutes 10 for a nonpositive `limit` and caps above 100 at
   100, requires `name`, rejects an unknown non-blank `type`, and requires a
   supported `type` or canonical content-entity handle for global calls; the
   context handler reads only the `entity_id` path parameter; the content
   read decodes only `entity_id` and rejects the empty string.

## Invariants

- Keep only entity-resolution family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution
  stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package entityresolutiontools`; the root
  imports it with an explicit alias.
- Preserve each tool's exact method, path, body keys, and query keys.
  `resolve_entity` and `get_entity_content` are `POST` with a nil query;
  `get_entity_context` is a `GET` with a nil body and an always-non-nil query
  map, empty when `environment` is blank.
- Preserve `resolve_entity`'s conditional keys exactly: `name` maps from the
  advertised `query` argument only when the deprecated `name` alias is blank
  and stays absent when both are blank; `type` prefers `type` and falls back
  to the first element of the deprecated `types` array, including to an
  explicit empty string for a non-string first element; `repo_id` travels
  only when non-empty. `limit` always travels, defaulting to 10.
- `get_entity_content` must keep sending `entity_id` even when blank, so the
  handler's HTTP 400 stays the visible failure rather than a silent shape
  change.
- Keep `url.PathEscape` on the `get_entity_context` entity id; the service
  tools' `normalizeQualifiedIdentifier` stripping is deliberately NOT applied
  here — entity ids are already canonical.
- Return the zero request and `handled=false` for unrelated tools, including
  `search_entity_content`, which shares the entity spelling but stays in the
  `content` child, which owns the shared `contentSearchBody` builder.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the advertised
  schema, because a mismatch changes a client-visible page size.
- Add a body key only after the handler decodes it by name; a key the handler
  does not read is inert — `environment` on the entity-context route is the
  standing example.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped `repo_id` or `type` never fails loudly on repository-scoped
  calls: the resolve handler accepts blanks there and silently widens the
  result. A dropped `limit` never fails either — the handler substitutes its
  own 10. The per-key assertions in both test files exist because a
  request-level comparison alone hides which key was lost.
- Sending `name` as an empty string instead of omitting it changes the bytes
  on the wire that the root tests pin; the presence assertions exist for
  exactly that regression.
- Dropping `url.PathEscape` breaks entity ids containing `/`, spaces, or
  other reserved characters into wrong paths that 404 at the mux instead of
  resolving; the escaping tests pin the escaped shape.
- Claiming a name the root also answers makes resolution depend on which
  check runs first — `search_entity_content` is the near-miss to watch.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `stringSlice` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not duplicate `contentSearchBody` here to absorb
  `search_entity_content`; the shared builder is the reason that arm stays at
  root, and a copy would let the two search tools' shared wire shape drift.
- Do not describe `environment` as a handler filter: the context handler
  never decodes it, and the docs and tests must keep that asymmetry.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, method, path, body key, query key, or default in a
  way clients can observe, including the conditional `name`, `type`, and
  `repo_id` keys and the conditional `environment` query parameter.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/entityresolution ./internal/mcp -count=1
go vet ./internal/mcp/entityresolution ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
