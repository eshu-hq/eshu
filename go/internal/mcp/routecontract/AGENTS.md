# AGENTS.md — MCP route contract guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for route resolution, HTTP dispatch, and transport rules.
3. `../dispatch.go` for the root route shape and dispatch path.
4. `../relationships/code_routes.go` and `../relationships/edge_routes.go` for
   the first family selectors.
5. `../dispatch_relationships.go` and `../dispatch_relationship_edges.go` for
   their root fanout adapters.
6. The owning domain route and its tests before changing an argument method.

## Invariants

- Keep this package dependency-neutral. Do not import the parent MCP package or
  a domain package.
- Preserve the exact accepted types, defaults, and nil behavior of every
  `Arguments` method. Domain routers rely on those coercions for wire
  compatibility.
- Keep `Request` limited to method, path, body, and query data.
- Tool names, family membership, route-selection policy, global fanout,
  adapters, and execution remain outside this package.

## Common changes

- Add an argument method only when a domain route needs a coercion that cannot
  stay local. Start with a focused failing test for every accepted and rejected
  input type.
- Add a request field only when the root `route` carries the same value and the
  adapter plus dispatch tests prove it survives unchanged.

## Failure modes

- Importing `internal/mcp` creates a parent-child cycle.
- Broadening a coercion can change a request body for callers that previously
  received a default.
- Copying an incoming `[]any` changes its current aliasing behavior.
- Putting route names here turns a value contract into a second registry.

## Anti-patterns

- Do not add HTTP clients, handler calls, authorization, logging, metrics, or
  storage access.
- Do not validate domain-specific values here.
- Do not register routes through package initialization.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/routecontract ./internal/mcp -count=1
go vet ./internal/mcp/routecontract ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root.
