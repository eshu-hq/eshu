# AGENTS.md — code MCP namespace guidance

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.

This directory is a documentation-only namespace. Keep route selection in
leaf packages, registration and dispatch in the parent `mcp` package, and
query execution in `internal/query/codequery`. Do not add imports,
declarations, initialization, shared mutable state, queue ownership, or
telemetry to this namespace.

Preserve the separation between MCP registration and request selection and
query execution; matching domain names do not make them one owner or a
shared package. Keep every tool name, request path, and body key stable.
