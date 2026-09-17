# AGENTS.md — supply-chain MCP namespace guidance

Read `README.md`, `doc.go`, and `../../AGENTS.md` before changing this tree.

This directory is a documentation-only namespace. Preserve the evidence and
impact package boundary; do not merge the siblings or share selection
helpers between them. Keep route selection in the leaves and registration
and dispatch in the parent `mcp` package. Do not add imports, declarations,
initialization, shared mutable state, queue ownership, or telemetry to this
namespace.
