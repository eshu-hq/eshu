# AGENTS.md — MCP relationship registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` and `../tools_codebase.go` for the ordered assembly positions.
4. The root relationship dispatch files and tests for route and body mapping.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../query/AGENTS.md` before changing the relationship-edge query path.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package; validation and graph reads stay in `internal/query`.
- Keep the package clause as `package relationshiptools`; the root imports it
  with an explicit alias.
- Preserve every tool name, description, schema, required verb, bounds, and
  canonical enum order.
- `CodeTools` returns exactly the story definition followed by the analysis
  definition. `AnalyzeCodeRelationshipsSchema` is the canonical analysis
  schema constructor used by `CodeTools`; root compatibility helpers delegate
  to it directly instead of inspecting the ordered registry. All constructors
  return fresh, deeply independent definitions or schemas on every call.
- Keep the code tools at zero-based root positions 8 and 9. Keep
  `list_relationship_edges` after `ask` and before `list_repository_files`.

## Common changes

- Change the schema only with the root route tests, query-handler contract,
  HTTP API reference, and golden-corpus query-shape proof.
- Update a serialized-definition hash only for an approved wire-contract
  change.
- Change the `source_tool` enum only through `sourcetool.Canonical` and keep the
  root canonical-vocabulary test.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving a relationship route here would pull root-only route and argument
  helpers across the ownership boundary.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses moving the tool away from its client-visible position.

## Anti-patterns

- Do not add route, HTTP, query, graph, storage, authorization, or telemetry
  helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root ordered-name hash guards.
- Do not duplicate or widen the canonical source-tool vocabulary here.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/relationships ./internal/mcp -count=1
go vet ./internal/mcp/relationships ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
relationship response-shape change also requires the golden-corpus gate
described by the parent package.
