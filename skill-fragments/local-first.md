---
id: local-first
version: 1.0.0
byte_citation: go/internal/semanticqueue/README.md#10
description: |
  Every fragment's default rendering works in local_lightweight and
  local_authoritative profiles without a configured LLM provider.
  LLM augmentation is policy-gated. llm:no-provider is a first-class
  status, not an error.
---

# Eshu Local-First

Eshu works without an LLM provider key.

Every fragment's default rendering MUST work in `local_lightweight`
and `local_authoritative` profiles without a configured LLM provider.
The deterministic query, graph, and CLI surfaces are the primary
interface. LLM augmentation is policy-gated and never the only path
to a correct answer.

The semantic queue recognizes the following planner labels:

- `llm:no-provider` — no LLM provider is configured for this run.
  This is a first-class status, not an error.
- `llm:policy-denied` — the active policy denied LLM augmentation
  for this chunk.
- `llm:budget-denied` — the configured budget exhausted before the
  chunk was processed.
- `llm:unsafe` — the guard preflight classified the chunk as unsafe
  for LLM augmentation.
- `llm:unchanged` — the chunk was processed but produced no change.
- `llm:changed` — the chunk was processed and produced a change.
- `deleted-source` — the source record was deleted between attempts.

These labels are deterministic, status-row safe, and low-cardinality
for telemetry.
