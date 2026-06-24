---
id: reducer-invariant
version: 1.0.0
byte_citation: docs/public/architecture.md#24-26
description: |
  Intake never writes graph state. The reducer is the only writer of
  canonical graph truth. MCP tool calls route through dispatchTool
  into the shared HTTP query handlers. Queue claims are leased,
  retryable, supersedable, and dead-letterable.
---

# Eshu Reducer Invariant

Eshu's intake services observe source truth and enqueue work. The
resolution engine (the reducer) is the only writer of canonical graph
state.

This is the single most important rule for the agent surface. It
means:

- An intake service (collector, ingester, projector) MUST NOT write
  to the shared graph. It only emits facts and enqueues work.
- The reducer owns the canonical graph truth. It applies a
  deterministic, idempotent projection from facts to graph nodes and
  edges.
- MCP tool calls MUST route through `dispatchTool` into the shared
  HTTP query handlers (`go/internal/mcp/dispatch.go:15`). The
  reducer's queue claims are leased, retryable, supersedable, and
  dead-letterable (`go/internal/storage/postgres/AGENTS.md:49-78`).

If a write appears in the wrong seam, the result is a non-idempotent
graph mutation that races with reducer projection. Treat that as a
P0 bug.
