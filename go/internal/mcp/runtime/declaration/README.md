# MCP runtime declaration tools

## Purpose

Owns the `list_runtime_version_declarations` tool definition and the pure
selection of its internal request (#8817). The tool answers questions such as
"which repositories declare a Node major below 20" from the declared runtime
versions the projection writes to the graph.

| Tool | Method and path |
| --- | --- |
| `list_runtime_version_declarations` | `POST /api/v0/runtime-declarations/query` |

## Boundary

This package selects the request and declares the tool. It runs no query. The
bounded graph read, the filters, the keyset cursor and the
`excluded_from_comparison` and `partial_reasons` contract live in
`go/internal/query/runtime/declaration`. The root `mcp` package splices the
registration (`types.go`) and adapts the route (`dispatch_repositories.go`).

## Contract notes

- The request body is the caller's arguments, unselected. The handler decodes
  strictly, so an unknown field or a wrong-typed value is a 400 that names it.
  Selecting keys here would silently drop a misspelled filter and answer with
  every declaration.
- The tool description tells the model to read `partial_reasons` before
  concluding a fleet is clean: range, alias and unresolved specs are excluded
  from a comparison, not guessed.

## Verification

```bash
cd go && go test ./internal/mcp/runtime/declaration ./internal/mcp -count=1
```
