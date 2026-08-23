# AGENTS.md — MCP tool contract guidance

## Scope

This directory owns the dependency-neutral MCP tool registration data shape.
It is below both `internal/mcp` and future domain registration packages in the
import graph.

## Invariants

- Keep this package data-only. Do not add routing, dispatch, transport,
  authorization, telemetry, storage, or query behavior.
- Preserve the JSON field names of `ToolDefinition`; they are part of the MCP
  `tools/list` wire contract.
- Keep `mcp.ToolDefinition` as a type alias so existing external callers remain
  source-compatible.
- Tool set membership and ordering remain owned by `mcp.ReadOnlyTools`.
- A registration-family move must prove the registered set and order are
  unchanged.

## Verification

Run `go test ./internal/mcp/... -count=1` from `go/`. Public shape changes also
require the MCP guide and shared HTTP/API contract review described by the
parent package instructions.
