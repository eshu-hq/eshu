# AGENTS.md — MCP semantic registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly positions.
4. `../dispatch_semantic_evidence.go` and
   `../dispatch_semantic_search.go` for the split route ownership.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../query/AGENTS.md` before changing semantic query behavior.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package; validation and reads stay in `internal/query`.
- Keep the package clause as `package semantictools`; the root imports it with
  an explicit alias.
- Preserve all three tool names, descriptions, schemas, and their local order.
- Return fresh definitions on every call.
- Keep the root order `investigation packets -> semantic evidence -> semantic
  search -> documentation finding aggregates` and the complete 162-tool order
  unchanged.
- Keep evidence routing in `dispatch_semantic_evidence.go` and search routing
  in `dispatch_semantic_search.go`.

## Common changes

- Change a schema only with the root route tests, query-handler contract, public
  API documentation, and golden-corpus query-shape proof.
- Update the serialized-definition hashes in the package tests only for an
  approved wire-contract change.
- Add a tool only with an explicit decision about its position in the root
  registry and its owning router.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving either router here would expose root-only route and argument helpers
  and blur registration ownership.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses local or root registration reordering.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, transport, or telemetry
  helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root order guards.
- Do not merge the two root routers as part of a registration move.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/semantic ./internal/mcp -count=1
go vet ./internal/mcp/semantic ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
semantic query-shape change also requires the golden-corpus gate described by
the parent package.
