# MCP Agent Rules

These rules are mandatory for changes under `go/internal/mcp`.

## Read First

1. `go/internal/mcp/README.md`
2. `go/internal/mcp/server.go`
3. `go/internal/mcp/dispatch.go`
4. `go/internal/mcp/dispatch_args.go`
5. `go/internal/mcp/types.go`
6. `go/internal/query/README.md`
7. `docs/public/mcp/index.md`
8. `docs/public/reference/mcp-cookbook.md`

## Invariants

- Every `ReadOnlyTools` entry MUST resolve to a dispatch route.
- MCP query truth MUST flow through the HTTP query handler passed to
  `NewServer`; do not add separate Postgres or graph queries here.
- Resource responses MUST use `query.EnvelopeMIMEType`.
- Authorization MUST pass through from the MCP request to the internal handler.
- Dispatch MUST request the canonical Eshu envelope with
  `Accept: application/eshu.envelope+json`.
- Service identifier paths MUST use `normalizeQualifiedIdentifier` before
  `PathEscape`.
- SSE channel overflow is non-fatal and logs a warning; callers MUST NOT assume
  every response arrives through SSE when the session buffer is saturated.

## Change Rules

- New MCP tool: add the tool definition, dispatch route, README table row,
  dispatch test, tool-count test update, cookbook/reference docs if public, and
  run `go test ./internal/mcp -count=1`.
- Existing argument mapping change: update `resolveRoute`, `InputSchema`, and
  focused dispatch tests together.
- New helper: add focused unit coverage because helper type errors silently
  become zero-value request bodies.
- SSE keepalive change: update server tests and README.
- Protocol version change: verify client compatibility and update public MCP
  docs.

## Failure Checks

- `unknown tool`: tool registry and `resolveRoute` are out of sync.
- Plain JSON response: query handler did not return the `{data, truth, error}`
  envelope shape.
- SSE no-response: inspect session channel saturation logs.
- Service tool 404: verify qualified identifiers are stripped before routing.
- Empty IaC reachability results: verify the Postgres-backed reachability store
  is wired in the binary.

## Forbidden Without Architecture-Owner Approval

- Tool names, required fields, or removal of any read-only tool.
- `parseCanonicalEnvelope` three-key detection logic.
- SSE endpoint event format or channel-backed session model.
- Raw Cypher or package-local graph traversal inside MCP dispatch.
- Manual envelope construction outside `internal/query`.
