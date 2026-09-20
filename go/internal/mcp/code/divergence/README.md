# codedivergencetools

## Purpose

`go/internal/mcp/code/divergence` owns the MCP divergence family: pure route
selection plus the tool definitions for `find_code_divergence` (repo-scoped
parallel-implementation report) and `investigate_code_divergence`
(single-finding drilldown with bounded follow-up calls).

## Where this fits

MCP tool call → root dispatch (`go/internal/mcp/dispatch.go`) → this
package's `Route` → internal HTTP POST to the query handler
(`go/internal/query/codequery/divergence.go`). This package runs no query
and must keep every tool name, request path, and body key stable.

## Notes

- Defaults agree with the handler: limit 25, offset 0, both kinds when kind
  is blank. Investigate requires an explicit kind.
- Tool names are single spellings; the family claims no aliases.

## Route map

| Tool | Method | Path |
| --- | --- | --- |
| `find_code_divergence` | POST | `/api/v0/code/divergence/findings` |
| `investigate_code_divergence` | POST | `/api/v0/code/divergence/investigate` |
