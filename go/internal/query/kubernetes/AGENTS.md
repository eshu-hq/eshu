# kubernetes — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for layout, the MCP registry
coupling, and the difference from `querycontract/kubernetes`.

## Invariants

- The route stays bounded: `limit` 1-200 is required and at least one anchor
  filter is required.
- Scoped callers bind `fact.scope_id`; an empty grant never reaches the store.
- Change the capability row only in `capability.go`.
- Keep the store interface name distinctive (see README); the MCP
  route-serves-data registry matches it by substring.
- This package may import `internal/query/supply/chain` for the probe port;
  it must not import the root query package.

## Common changes

Moving or renaming a file or type here: update the `internal/mcp`
route-serves-data registry files named in the README in the same change, then
run `go test ./internal/mcp`.
