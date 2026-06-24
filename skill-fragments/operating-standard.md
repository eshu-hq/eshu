---
id: operating-standard
version: 1.0.0
requires:
  - truth-labels
byte_citation: docs/internal/agent-guide.md#14-22
description: |
  Eshu's operating order is fixed: accuracy, then performance, then
  concurrency. An agent MUST prove correctness before proving speed.
---

# Eshu Operating Standard

For runtime work the order is fixed.

1. **Accuracy** — persisted facts, graph truth, API/MCP/CLI truth, and
   fixture intent agree. The graph, the query, and the deployment tell
   the same story.
2. **Performance** — the correct path has a before/after or
   no-regression measurement on the same input shape. The cost of the
   happy path is measured, not assumed.
3. **Concurrency** — idempotency, retry boundaries, claim ordering,
   transaction scope, conflict keys, and dead-letter behavior hold
   under intended worker counts. The path is safe under contention.

If any one of the three fails, the work is not done.
