# AGENTS.md — admission MCP namespace guidance

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.

This directory is a documentation-only namespace. Keep route selection in
the leaf package, registration and dispatch in the parent `mcp` package,
and admission evaluation in the reducer. Do not add imports, declarations,
initialization, shared mutable state, queue ownership, or telemetry to this
namespace. Keep every tool name, request path, and body key stable.
