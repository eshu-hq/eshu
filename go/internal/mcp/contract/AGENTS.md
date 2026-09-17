# AGENTS.md — contract MCP namespace guidance

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.

This directory is a documentation-only namespace for MCP-owned shared
contracts. Preserve the dependency direction: families and root import the
contracts, never the reverse. Do not add routing, dispatch, transport,
authorization, telemetry, storage, or query behavior to this namespace, and
do not leave an old-path forwarding package or duplicate contract types.
Keep every tool name, request path, and body key stable.
