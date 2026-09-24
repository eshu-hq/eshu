# observability/coverage — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for layout and the MCP registry
coupling.

## Invariants

- The route stays bounded: at least one anchor filter is required, limit
  1-200.
- Scoped callers bind `fact.scope_id`; an empty grant never reaches the store.
- Change the capability row only in `capability.go`.
- Keep the store interface name distinctive (see README); the MCP
  route-serves-data registry matches it by name.
- This package must not import the root query package or another handler
  family.

## Common changes

Moving or renaming a file or type here: update
`internal/mcp/route_serves_data_registry_routes.go` and
`internal/mcp/route_serves_data_registry.go` in the same change, then run
`go test ./internal/mcp`.
